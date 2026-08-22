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
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/triedb/database"
)

type paraReader struct {
	accountTrie *reader
	readers     []*reader
}

func (this *paraReader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	if owner == (common.Hash{}) {
		return this.accountTrie.Node(owner, path, hash)
	}
	index := getShardIndexFromHash(owner, len(this.readers))
	return this.readers[index].Node(owner, path, hash)
}

func (this *paraReader) AccountRLP(hash common.Hash) ([]byte, error) {
	return this.readers[getShardIndexFromHash(hash, len(this.readers))].AccountRLP(hash)
}

func (this *paraReader) Account(hash common.Hash) (*types.SlimAccount, error) {
	return this.readers[getShardIndexFromHash(hash, len(this.readers))].Account(hash)
}

func (this *paraReader) Storage(accountHash, storageHash common.Hash) ([]byte, error) {
	index := getShardIndexFromHash(accountHash, len(this.readers))
	return this.readers[index].Storage(accountHash, storageHash)
}

// NodeReader retrieves a layer belonging to the given state root.
func (db *ParaDatabase) NodeReader(root common.Hash) (database.NodeReader, error) {
	reader, err := db.reader(root)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func (db *ParaDatabase) StateReader(root common.Hash) (database.StateReader, error) {
	reader, err := db.reader(root)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func (db *ParaDatabase) reader(root common.Hash) (*paraReader, error) {
	if len(db.databases) == 0 {
		return nil, fmt.Errorf("parallel path database has no shards")
	}
	if db.accountTrie == nil {
		return nil, fmt.Errorf("open state %#x: account trie is nil", root)
	}
	accountValue, err := db.accountTrie.NodeReader(root)
	if err != nil {
		return nil, fmt.Errorf("open state %#x on account trie: %w", root, err)
	}
	accountReader, ok := accountValue.(*reader)
	if !ok {
		return nil, fmt.Errorf("open state %#x on account trie: unexpected reader type %T", root, accountValue)
	}
	result := &paraReader{
		accountTrie: accountReader,
		readers:     make([]*reader, len(db.databases)),
	}
	for shard, shardDB := range db.databases {
		if shardDB == nil {
			return nil, fmt.Errorf("open state %#x on shard %d: database is nil", root, shard)
		}
		value, err := shardDB.NodeReader(root)
		if err != nil {
			return nil, fmt.Errorf("open state %#x on shard %d: %w", root, shard, err)
		}
		reader, ok := value.(*reader)
		if !ok {
			return nil, fmt.Errorf("open state %#x on shard %d: unexpected reader type %T", root, shard, value)
		}
		result.readers[shard] = reader
	}
	return result, nil
}
