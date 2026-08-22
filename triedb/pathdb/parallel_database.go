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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

const configFileName = "parallel_config.toml"
const accountTrieDirectory = "account-trie"

// ParaDatabase owns the numbered data shards and one standalone account trie.
type ParaDatabase struct {
	diskdb        ethdb.Database // Original database, used for shared contract code
	accountDiskDB ethdb.Database
	accountTrie   *Database
	diskdbs       []ethdb.Database
	databases     []*Database
}

// NewParaDatabase has the same inputs as New so ParaPathDB can be used in its
// place. Storage-trie nodes and flat state live in the numbered Pebble shards,
// account-trie nodes live in a standalone Pebble database, and the supplied
// database remains the shared source for contract code.
//
// It creates <path>/0 through <path>/<num-shards-1>, plus
// <path>/account-trie. The account trie is not included in num-shards.
// Configuration or database-open failures are fatal because the
// pathdb-compatible constructor cannot return an error.
func NewParaDatabase(diskdb ethdb.Database, pathConfig *Config, isVerkle bool) *ParaDatabase {
	config, err := loadConfig(configFileName)
	if err != nil {
		panic(fmt.Errorf("create parallel path database: %w", err))
	}

	shardConfig := config.ParaDatabase
	if pathConfig != nil && pathConfig.ReadOnly {
		shardConfig.ReadOnly = true
	}
	paraDB := &ParaDatabase{
		diskdb:    diskdb,
		diskdbs:   make([]ethdb.Database, shardConfig.NumShards),
		databases: make([]*Database, shardConfig.NumShards),
	}
	accountDiskDB, accountTrie, err := initializeAccountTrieDatabase(
		shardConfig,
		databasePathConfig(pathConfig, accountTrieDirectory, shardConfig.ReadOnly),
		isVerkle,
	)
	if err != nil {
		panic(fmt.Errorf("create parallel account trie: %w", err))
	}
	paraDB.accountDiskDB = accountDiskDB
	paraDB.accountTrie = accountTrie

	for shard := 0; shard < shardConfig.NumShards; shard++ {
		shardName := strconv.Itoa(shard)
		diskdb, database, err := initializeShardDatabase(
			shardName,
			shardConfig,
			databasePathConfig(pathConfig, shardName, shardConfig.ReadOnly),
			isVerkle,
			accountDiskDB,
		)

		if err != nil {
			_ = paraDB.Close()
			panic(fmt.Errorf("create parallel path database: %w", err))
		}
		paraDB.diskdbs[shard] = diskdb
		paraDB.databases[shard] = database
	}
	return paraDB
}

func (db *ParaDatabase) Close() error {
	if db == nil {
		return nil
	}
	var errs []error
	for _, database := range db.databases {
		if database != nil {
			errs = append(errs, database.Close())
		}
	}
	if db.accountTrie != nil {
		errs = append(errs, db.accountTrie.Close())
	}
	for _, diskdb := range db.diskdbs {
		if diskdb != nil {
			errs = append(errs, diskdb.Close())
		}
	}
	if db.accountDiskDB != nil {
		errs = append(errs, db.accountDiskDB.Close())
	}
	return errors.Join(errs...)
}

func (db *ParaDatabase) Size() (diffs common.StorageSize, nodes common.StorageSize) {
	if db.accountTrie != nil {
		d, n := db.accountTrie.Size()
		diffs += d
		nodes += n
	}
	for i := range db.databases {
		d, n := db.databases[i].Size()
		diffs += d
		nodes += n
	}
	return diffs, nodes
}

// Update partitions the transition and advances every data shard and the
// standalone account trie to the same root.
func (db *ParaDatabase) Update(root common.Hash, parentRoot common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *StateSetWithOrigin) error {
	count := len(db.databases)
	if count == 0 {
		return errors.New("parallel path database has no shards")
	}
	nodeGroups, accountNodes := nodes.Regroup(count)
	stateGroups := states.regroup(count)
	rawStorageKey := false
	if states != nil {
		rawStorageKey = states.rawStorageKey
	}
	accountStates := NewStateSetWithOrigin(nil, nil, nil, nil, rawStorageKey)
	return db.forEachDatabaseDo("update", func(shard int, database *Database) error {
		if shard == count {
			return database.Update(root, parentRoot, block, accountNodes, accountStates)
		}
		return database.Update(root, parentRoot, block, nodeGroups[shard], stateGroups[shard])
	})
}

// Commit flushes the requested root on all data shards and the account trie.
func (db *ParaDatabase) Commit(root common.Hash, report bool) error {
	return db.forEachDatabaseDo("commit", func(_ int, database *Database) error {
		return database.Commit(root, report)
	})
}

// Journal persists the in-memory layers of all data shards and the account trie.
func (db *ParaDatabase) Journal(root common.Hash) error {
	return db.forEachDatabaseDo("journal", func(_ int, database *Database) error {
		return database.Journal(root)
	})
}

// Disable deactivates all data shards and the account trie during state sync.
func (db *ParaDatabase) Disable() error {
	return db.forEachDatabaseDo("disable", func(_ int, database *Database) error {
		return database.Disable()
	})
}

// Enable reactivates all data shards and the account trie at the supplied root.
func (db *ParaDatabase) Enable(root common.Hash) error {
	return db.forEachDatabaseDo("enable", func(_ int, database *Database) error {
		return database.Enable(root)
	})
}

// Recover rolls all data shards and the account trie back to the target state.
func (db *ParaDatabase) Recover(root common.Hash) error {
	return db.forEachDatabaseDo("recover", func(_ int, database *Database) error {
		return database.Recover(root)
	})
}

// Recoverable reports true only when every data shard and the account trie can
// recover the state.
func (db *ParaDatabase) Recoverable(root common.Hash) bool {
	if len(db.databases) == 0 {
		return false
	}
	if db.accountTrie == nil || !db.accountTrie.Recoverable(root) {
		return false
	}
	for _, database := range db.databases {
		if !database.Recoverable(root) {
			return false
		}
	}
	return true
}

// IndexProgress returns the largest remaining indexing backlog.
func (db *ParaDatabase) IndexProgress() (uint64, error) {
	var maximum uint64
	if db.accountTrie == nil {
		return 0, errors.New("parallel path database has no account trie")
	}
	progress, err := db.accountTrie.IndexProgress()
	if err != nil {
		return 0, fmt.Errorf("account trie index progress: %w", err)
	}
	maximum = progress
	for shard, database := range db.databases {
		progress, err := database.IndexProgress()
		if err != nil {
			return 0, fmt.Errorf("index progress on shard %d: %w", shard, err)
		}
		if progress > maximum {
			maximum = progress
		}
	}
	return maximum, nil
}

// VerifyState regenerates the global state root from the merged shard
// iterators. Contract code remains in the original database supplied by triedb.
func (db *ParaDatabase) VerifyState(root common.Hash) error {
	accounts, err := db.AccountIterator(root, common.Hash{})
	if err != nil {
		return err
	}
	defer accounts.Release()
	got, err := generateTrieRoot(accounts, common.Hash{}, stackTrieHasher, func(accountHash, codeHash common.Hash, stat *generateStats) (common.Hash, error) {
		if codeHash != types.EmptyCodeHash {
			if db.diskdb == nil || len(rawdb.ReadCode(db.diskdb, codeHash)) == 0 {
				return common.Hash{}, errors.New("failed to read contract code")
			}
		}
		storage, err := db.StorageIterator(root, accountHash, common.Hash{})
		if err != nil {
			return common.Hash{}, err
		}
		defer storage.Release()
		return generateTrieRoot(storage, accountHash, stackTrieHasher, nil, stat, false)
	}, newGenerateStats(), true)
	if err != nil {
		return err
	}
	if got != root {
		return fmt.Errorf("state root hash mismatch: got %x, want %x", got, root)
	}
	return nil
}

// forEachDatabaseDo runs an operation concurrently across all data shards and
// the standalone account trie. The account trie uses index len(databases).
func (db *ParaDatabase) forEachDatabaseDo(operation string, fn func(int, *Database) error) error {
	if len(db.databases) == 0 {
		return errors.New("parallel path database has no shards")
	}

	count := len(db.databases)
	errs := make([]error, count+1)
	tasks := make([]func(), count+1)
	for shard, database := range db.databases {
		shard, database := shard, database
		tasks[shard] = func() {
			if database == nil {
				errs[shard] = fmt.Errorf("%s shard %d: database is nil", operation, shard)
				return
			}
			if err := fn(shard, database); err != nil {
				errs[shard] = fmt.Errorf("%s shard %d: %w", operation, shard, err)
			}
		}
	}

	tasks[count] = func() {
		if db.accountTrie == nil {
			errs[count] = fmt.Errorf("%s account trie: database is nil", operation)
			return
		}
		if err := fn(count, db.accountTrie); err != nil {
			errs[count] = fmt.Errorf("%s account trie: %w", operation, err)
		}
	}

	ParallelExecute(tasks...)
	return errors.Join(errs...)
}

// databasePathConfig gives every data shard and the standalone account trie a
// distinct journal directory. The caller's config is never mutated.
func databasePathConfig(config *Config, name string, readOnly bool) *Config {
	if config == nil {
		config = Defaults
	}
	shardConfig := *config
	shardConfig.ReadOnly = readOnly
	if config.JournalDirectory != "" {
		shardConfig.JournalDirectory = filepath.Join(config.JournalDirectory, name)
	}
	return &shardConfig
}

// initializeAccountTrieDatabase opens the standalone account-trie database.
// Unlike a data shard, it owns and resolves its own persistent trie root.
func initializeAccountTrieDatabase(
	config ParaDatabaseConfig,
	pathConfig *Config,
	isVerkle bool,
) (ethdb.Database, *Database, error) {
	diskdb, err := openParallelDiskDatabase(accountTrieDirectory, config)
	if err != nil {
		return nil, nil, err
	}
	return diskdb, New(diskdb, pathConfig, isVerkle), nil
}

// initializeShardDatabase opens a numbered data shard. Its persistent state root
// is always resolved from the standalone account-trie database.
func initializeShardDatabase(
	name string,
	config ParaDatabaseConfig,
	pathConfig *Config,
	isVerkle bool,
	accountDiskDB ethdb.Database,
) (ethdb.Database, *Database, error) {
	if accountDiskDB == nil {
		return nil, nil, errors.New("initialize data shard: account-trie database is nil")
	}
	diskdb, err := openParallelDiskDatabase(name, config)
	if err != nil {
		return nil, nil, err
	}
	return diskdb, newDatabase(diskdb, accountDiskDB, pathConfig, isVerkle), nil
}

// openParallelDiskDatabase creates the directory and opens its Pebble database.
func openParallelDiskDatabase(name string, config ParaDatabaseConfig) (ethdb.Database, error) {
	databasePath := filepath.Join(config.Path, name)
	if err := os.MkdirAll(databasePath, 0o755); err != nil {
		return nil, fmt.Errorf("create parallel database directory %q: %w", databasePath, err)
	}

	kvdb, err := pebble.New(
		databasePath,
		config.CacheMB,
		config.Handles,
		fmt.Sprintf("parapathdb/%s", name),
		config.ReadOnly,
	)
	if err != nil {
		return nil, fmt.Errorf("open parallel Pebble database %q: %w", databasePath, err)
	}

	// PathDB discovers its state-history freezer through AncientDatadir. The
	// chain ancient APIs remain unsupported, but every shard gets a distinct
	// state-history root next to its Pebble database.
	diskdb := &shardDiskDatabase{
		Database: rawdb.NewDatabase(kvdb),
		ancient:  filepath.Join(databasePath, "ancient"),
	}
	return diskdb, nil
}

type shardDiskDatabase struct {
	ethdb.Database
	ancient string
}

func (db *shardDiskDatabase) AncientDatadir() (string, error) {
	return db.ancient, nil
}
