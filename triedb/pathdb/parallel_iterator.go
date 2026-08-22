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
)

// AccountIterator merges the sorted account streams from all shards.
func (db *ParaDatabase) AccountIterator(root common.Hash, seek common.Hash) (AccountIterator, error) {
	if len(db.databases) == 0 {
		return nil, fmt.Errorf("parallel path database has no shards")
	}
	iterators := make([]AccountIterator, 0, len(db.databases))
	for shard, database := range db.databases {
		iterator, err := database.AccountIterator(root, seek)
		if err != nil {
			for _, opened := range iterators {
				opened.Release()
			}
			return nil, fmt.Errorf("open account iterator on shard %d: %w", shard, err)
		}
		iterators = append(iterators, iterator)
	}
	return &parallelAccountIterator{
		iterators: iterators,
		active:    make([]bool, len(iterators)),
		current:   -1,
	}, nil
}

// StorageIterator routes an account's complete storage trie to its shard.
func (db *ParaDatabase) StorageIterator(root common.Hash, account common.Hash, seek common.Hash) (StorageIterator, error) {
	if len(db.databases) == 0 {
		return nil, fmt.Errorf("parallel path database has no shards")
	}
	shard := getShardIndexFromHash(account, len(db.databases))
	iterator, err := db.databases[shard].StorageIterator(root, account, seek)
	if err != nil {
		return nil, fmt.Errorf("open storage iterator on shard %d: %w", shard, err)
	}
	return iterator, nil
}

type parallelAccountIterator struct {
	iterators   []AccountIterator
	active      []bool
	current     int
	initialized bool
	fail        error
	released    bool
}

func (it *parallelAccountIterator) Next() bool {
	if it.released || it.fail != nil {
		return false
	}
	if !it.initialized {
		it.initialized = true
		for shard := range it.iterators {
			it.active[shard] = it.advance(shard)
			if it.fail != nil {
				return false
			}
		}
	} else if it.current >= 0 {
		it.active[it.current] = it.advance(it.current)
		if it.fail != nil {
			return false
		}
	}
	it.current = -1
	for shard, active := range it.active {
		if !active {
			continue
		}
		if it.current == -1 || it.iterators[shard].Hash().Cmp(it.iterators[it.current].Hash()) < 0 {
			it.current = shard
		}
	}
	return it.current >= 0
}

func (it *parallelAccountIterator) advance(shard int) bool {
	if it.iterators[shard].Next() {
		return true
	}
	if err := it.iterators[shard].Error(); err != nil {
		it.fail = fmt.Errorf("account iterator shard %d: %w", shard, err)
	}
	return false
}

func (it *parallelAccountIterator) Error() error {
	return it.fail
}

func (it *parallelAccountIterator) Hash() common.Hash {
	if it.current < 0 {
		return common.Hash{}
	}
	return it.iterators[it.current].Hash()
}

func (it *parallelAccountIterator) Account() []byte {
	if it.current < 0 {
		return nil
	}
	return it.iterators[it.current].Account()
}

func (it *parallelAccountIterator) Release() {
	if it.released {
		return
	}
	it.released = true
	for _, iterator := range it.iterators {
		iterator.Release()
	}
}
