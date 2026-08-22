/*
 *   Copyright (c) 2026 Arcology Network

 *   This program is free software: you can redistribute it and/or modify
 *   it under the terms of the GNU General Public License as published by
 *   the Free Software Foundation, either version 3 of the License, or
 *   (at your option) any later version.

 *   This program is distributed in the hope that it will be useful,
 *   but WITHOUT ANY WARRANTY; without even the implied warranty of
 *   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *   GNU General Public License for more details.

 *   You should have received a copy of the GNU General Public License
 *   along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package pathdb

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

func TestInitializeParallelDatabases(t *testing.T) {
	root := t.TempDir()
	journalRoot := t.TempDir()
	config := ParaDatabaseConfig{
		Engine:    "pebble",
		NumShards: 1,
		Path:      root,
		CacheMB:   16,
		Handles:   16,
	}
	pathConfig := &Config{JournalDirectory: journalRoot}

	accountDiskDB, accountTrie, err := initializeAccountTrieDatabase(
		config,
		databasePathConfig(pathConfig, accountTrieDirectory, false),
		false,
	)
	if err != nil {
		t.Fatalf("initialize account trie: %v", err)
	}
	diskdb, database, err := initializeShardDatabase("2", config, databasePathConfig(pathConfig, "2", false), false, accountDiskDB)
	if err != nil {
		_ = accountTrie.Close()
		_ = accountDiskDB.Close()
		t.Fatalf("initialize data shard: %v", err)
	}
	db := &ParaDatabase{
		accountDiskDB: accountDiskDB,
		accountTrie:   accountTrie,
		diskdbs:       []ethdb.Database{diskdb},
		databases:     []*Database{database},
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	path := filepath.Join(root, "2")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat shard directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("shard path is not a directory")
	}
	accountPath := filepath.Join(root, accountTrieDirectory)
	if info, err := os.Stat(accountPath); err != nil {
		t.Fatalf("stat account-trie directory: %v", err)
	} else if !info.IsDir() {
		t.Fatal("account-trie path is not a directory")
	}
	if got, want := database.config.JournalDirectory, filepath.Join(journalRoot, "2"); got != want {
		t.Fatalf("journal directory = %q, want %q", got, want)
	}
	if got, want := accountTrie.config.JournalDirectory, filepath.Join(journalRoot, accountTrieDirectory); got != want {
		t.Fatalf("account-trie journal directory = %q, want %q", got, want)
	}
	if database.rootdb != accountDiskDB {
		t.Fatal("data shard does not use the standalone account database as its root source")
	}
	if pathConfig.JournalDirectory != journalRoot {
		t.Fatalf("original journal directory changed to %q", pathConfig.JournalDirectory)
	}
	if database.stateFreezer == nil {
		t.Fatal("state history freezer was not initialized")
	}
	if got, err := diskdb.AncientDatadir(); err != nil {
		t.Fatalf("resolve shard ancient directory: %v", err)
	} else if want := filepath.Join(root, "2", "ancient"); got != want {
		t.Fatalf("ancient directory = %q, want %q", got, want)
	}
}

func TestParallelUpdateAndStateReaderRouting(t *testing.T) {
	db := newTestParaDatabase(t, 2)
	root := common.Hash{0xaa}
	account0 := common.Hash{0x02}
	account1 := common.Hash{0x03}
	slot := common.Hash{0x44}
	states := NewStateSetWithOrigin(
		map[common.Hash][]byte{account0: {0x10}, account1: {0x11}},
		map[common.Hash]map[common.Hash][]byte{account1: {slot: {0x22}}},
		nil,
		nil,
		false,
	)
	if err := db.Update(root, types.EmptyRootHash, 1, trienode.NewMergedNodeSet(), states); err != nil {
		t.Fatalf("update parallel database: %v", err)
	}

	stateReader, err := db.StateReader(root)
	if err != nil {
		t.Fatalf("open state reader: %v", err)
	}
	parallelReader := stateReader.(*paraReader)
	if blob, err := parallelReader.AccountRLP(account0); err != nil || !bytes.Equal(blob, []byte{0x10}) {
		t.Fatalf("read shard-0 account: blob %x, err %v", blob, err)
	}
	if blob, err := parallelReader.AccountRLP(account1); err != nil || !bytes.Equal(blob, []byte{0x11}) {
		t.Fatalf("read shard-1 account: blob %x, err %v", blob, err)
	}
	if blob, err := parallelReader.Storage(account1, slot); err != nil || !bytes.Equal(blob, []byte{0x22}) {
		t.Fatalf("read shard-1 storage: blob %x, err %v", blob, err)
	}

	wrongShard, err := db.databases[0].StateReader(root)
	if err != nil {
		t.Fatalf("open shard-0 reader: %v", err)
	}
	if blob, err := wrongShard.(*reader).AccountRLP(account1); blob != nil {
		t.Fatalf("account leaked into wrong shard: blob %x, err %v", blob, err)
	}

	iterator, err := db.AccountIterator(root, common.Hash{})
	if err != nil {
		t.Fatalf("open parallel account iterator: %v", err)
	}
	defer iterator.Release()
	var hashes []common.Hash
	for iterator.Next() {
		hashes = append(hashes, iterator.Hash())
	}
	if err := iterator.Error(); err != nil {
		t.Fatalf("iterate accounts: %v", err)
	}
	if len(hashes) != 2 || hashes[0] != account0 || hashes[1] != account1 {
		t.Fatalf("iterated account hashes = %x, want [%x %x]", hashes, account0, account1)
	}
}

func TestParallelNodeRegroup(t *testing.T) {
	rootSet := trienode.NewNodeSet(common.Hash{})
	owner := common.Hash{0x03}
	storageSet := trienode.NewNodeSet(owner)
	nodes := trienode.NewMergedNodeSet()
	if err := nodes.Merge(rootSet); err != nil {
		t.Fatal(err)
	}
	if err := nodes.Merge(storageSet); err != nil {
		t.Fatal(err)
	}

	groups, accountTrie := nodes.Regroup(2)
	if accountTrie.Sets[common.Hash{}] != rootSet {
		t.Fatal("account trie missing from standalone node set")
	}
	for shard := range groups {
		if _, exists := groups[shard].Sets[common.Hash{}]; exists {
			t.Fatalf("account trie leaked into data shard %d", shard)
		}
	}
	if groups[1].Sets[owner] != storageSet {
		t.Fatal("storage trie routed to wrong shard")
	}
	if _, exists := groups[0].Sets[owner]; exists {
		t.Fatal("storage trie duplicated into wrong shard")
	}
}

func TestParallelAccountTrieIsStandalone(t *testing.T) {
	db := newTestParaDatabase(t, 2)
	accountBlob := []byte{0xc0}
	root := crypto.Keccak256Hash(accountBlob)
	accountSet := trienode.NewNodeSet(common.Hash{})
	accountSet.AddNode(nil, trienode.NewNodeWithPrev(root, accountBlob, nil))

	owner := common.Hash{0x03}
	storagePath := []byte{0x01}
	storageBlob := []byte{0x80}
	storageSet := trienode.NewNodeSet(owner)
	storageSet.AddNode(storagePath, trienode.NewNodeWithPrev(crypto.Keccak256Hash(storageBlob), storageBlob, nil))

	nodes := trienode.NewMergedNodeSet()
	if err := nodes.Merge(accountSet); err != nil {
		t.Fatal(err)
	}
	if err := nodes.Merge(storageSet); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(root, types.EmptyRootHash, 1, nodes, NewStateSetWithOrigin(nil, nil, nil, nil, false)); err != nil {
		t.Fatalf("update parallel database: %v", err)
	}
	if err := db.Commit(root, false); err != nil {
		t.Fatalf("commit parallel database: %v", err)
	}

	if got := rawdb.ReadAccountTrieNode(db.accountDiskDB, nil); !bytes.Equal(got, accountBlob) {
		t.Fatalf("standalone account root = %x, want %x", got, accountBlob)
	}
	for shard, diskdb := range db.diskdbs {
		if got := rawdb.ReadAccountTrieNode(diskdb, nil); len(got) != 0 {
			t.Fatalf("account root leaked into data shard %d: %x", shard, got)
		}
	}
	storageShard := getShardIndexFromHash(owner, len(db.diskdbs))
	if got := rawdb.ReadStorageTrieNode(db.diskdbs[storageShard], owner, storagePath); !bytes.Equal(got, storageBlob) {
		t.Fatalf("storage node in shard %d = %x, want %x", storageShard, got, storageBlob)
	}
	if got := rawdb.ReadStorageTrieNode(db.accountDiskDB, owner, storagePath); len(got) != 0 {
		t.Fatalf("storage node leaked into account trie: %x", got)
	}

	// Reopening a data shard must recover the persistent state root from the
	// standalone account database, even though the shard has no account root.
	reopened := newDatabase(
		db.diskdbs[storageShard],
		db.accountDiskDB,
		&Config{NoAsyncGeneration: true, NoAsyncFlush: true},
		false,
	)
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("close reopened data shard: %v", err)
		}
	})
	reader, err := reopened.NodeReader(root)
	if err != nil {
		t.Fatalf("open reopened data-shard reader: %v", err)
	}
	storageHash := crypto.Keccak256Hash(storageBlob)
	if got, err := reader.Node(owner, storagePath, storageHash); err != nil || !bytes.Equal(got, storageBlob) {
		t.Fatalf("read reopened storage node: blob %x, err %v", got, err)
	}
}

func TestParallelStateRegroupKeepsOriginsWithAccount(t *testing.T) {
	address := common.HexToAddress("0x1234")
	accountHash := crypto.Keccak256Hash(address.Bytes())
	set := NewStateSetWithOrigin(
		map[common.Hash][]byte{accountHash: {0x01}},
		nil,
		map[common.Address][]byte{address: {0x02}},
		nil,
		false,
	)
	groups := set.regroup(7)
	shard := getShardIndexFromHash(accountHash, len(groups))
	if !bytes.Equal(groups[shard].accountData[accountHash], []byte{0x01}) {
		t.Fatal("account data routed to wrong shard")
	}
	if !bytes.Equal(groups[shard].accountOrigin[address], []byte{0x02}) {
		t.Fatal("account origin routed to wrong shard")
	}
	for other := range groups {
		if other == shard {
			continue
		}
		if _, exists := groups[other].accountData[accountHash]; exists {
			t.Fatalf("account data duplicated into shard %d", other)
		}
		if _, exists := groups[other].accountOrigin[address]; exists {
			t.Fatalf("account origin duplicated into shard %d", other)
		}
	}
}

func TestParallelNodeReaderMissingRootReturnsError(t *testing.T) {
	db := newTestParaDatabase(t, 2)
	root := common.Hash{0xaa}
	if err := db.accountTrie.Update(root, types.EmptyRootHash, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil, nil, nil, false)); err != nil {
		t.Fatalf("update account trie: %v", err)
	}
	if err := db.databases[0].Update(root, types.EmptyRootHash, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil, nil, nil, false)); err != nil {
		t.Fatalf("update first shard: %v", err)
	}
	if reader, err := db.NodeReader(root); err == nil || reader != nil {
		t.Fatalf("NodeReader() = (%T, %v), want nil reader and error", reader, err)
	}
}

func newTestParaDatabase(t *testing.T, shards int) *ParaDatabase {
	t.Helper()
	accountDiskDB := rawdb.NewMemoryDatabase()
	config := &Config{NoAsyncGeneration: true, NoAsyncFlush: true}
	db := &ParaDatabase{
		accountDiskDB: accountDiskDB,
		accountTrie:   New(accountDiskDB, config, false),
		diskdbs:       make([]ethdb.Database, shards),
		databases:     make([]*Database, shards),
	}
	for shard := 0; shard < shards; shard++ {
		diskdb := rawdb.NewMemoryDatabase()
		db.diskdbs[shard] = diskdb
		db.databases[shard] = newDatabase(diskdb, accountDiskDB, config, false)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close parallel database: %v", err)
		}
	})
	return db
}
