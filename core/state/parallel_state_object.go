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

package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

type storageTrieBatcher interface {
	UpdateStorageBatch([]trie.StorageUpdate) error
}

// updateTrie is responsible for persisting cached storage changes into the
// object's storage trie. In case the storage trie is not yet loaded, this
// function will load the trie automatically. If any issues arise during the
// loading or updating of the trie, an error will be returned. Furthermore,
// this function will return the mutated storage trie, or nil if there is no
// storage change at all.
//
// It assumes all the dirty storage slots have been finalized before.
func (s *stateObject) updateTrie() (Trie, error) {
	// Short circuit if nothing was accessed, don't trigger a prefetcher warning
	if len(s.uncommittedStorage) == 0 {
		// Nothing was written, so we could stop early. Unless we have both reads
		// and witness collection enabled, in which case we need to fetch the trie.
		if s.db.witness == nil || len(s.originStorage) == 0 {
			return s.trie, nil
		}
	}
	// Retrieve a pretecher populated trie, or fall back to the database. This will
	// block until all prefetch tasks are done, which are needed for witnesses even
	// for unmodified state objects.
	tr := s.getPrefetchedTrie()
	if tr != nil {
		// Prefetcher returned a live trie, swap it out for the current one
		s.trie = tr
	} else {
		// Fetcher not running or empty trie, fallback to the database trie
		var err error
		tr, err = s.getTrie()
		if err != nil {
			s.db.setError(err)
			return nil, err
		}
	}
	// Short circuit if nothing changed, don't bother with hashing anything
	if len(s.uncommittedStorage) == 0 {
		return s.trie, nil
	}
	// Perform trie updates before deletions. This prevents resolution of unnecessary trie nodes
	// in circumstances similar to the following:
	//
	// Consider nodes `A` and `B` who share the same full node parent `P` and have no other siblings.
	// During the execution of a block:
	// - `A` is deleted,
	// - `C` is created, and also shares the parent `P`.
	// If the deletion is handled first, then `P` would be left with only one child, thus collapsed
	// into a shortnode. This requires `B` to be resolved from disk.
	// Whereas if the created node is handled first, then the collapse is avoided, and `B` is not resolved.
	var (
		batcher, batch = tr.(storageTrieBatcher)
		deletions      []common.Hash
		updates        = make([]trie.StorageUpdate, 0, len(s.uncommittedStorage))
		updateCount    int64
		used           = make([]common.Hash, 0, len(s.uncommittedStorage))
	)
	batch = batch && !s.db.singlethreaded
	for key, origin := range s.uncommittedStorage {
		// Skip noop changes, persist actual changes
		value, exist := s.pendingStorage[key]
		if value == origin {
			log.Error("Storage update was noop", "address", s.address, "slot", key)
			continue
		}
		if !exist {
			log.Error("Storage slot is not found in pending area", s.address, "slot", key)
			continue
		}
		if (value != common.Hash{}) {
			if batch {
				updates = append(updates, trie.StorageUpdate{
					Key:   common.CopyBytes(key[:]),
					Value: common.CopyBytes(common.TrimLeftZeroes(value[:])),
				})
				updateCount++
			} else {
				if err := tr.UpdateStorage(s.address, key[:], common.TrimLeftZeroes(value[:])); err != nil {
					s.db.setError(err)
					return nil, err
				}
				s.db.StorageUpdated.Add(1)
			}
		} else {
			deletions = append(deletions, key)
		}
		// Cache the items for preloading
		used = append(used, key) // Copy needed for closure
	}
	if batch {
		for _, key := range deletions {
			updates = append(updates, trie.StorageUpdate{
				Key:    common.CopyBytes(key[:]),
				Delete: true,
			})
		}
		if err := batcher.UpdateStorageBatch(updates); err != nil {
			s.db.setError(err)
			return nil, err
		}
		s.db.StorageUpdated.Add(updateCount)
		s.db.StorageDeleted.Add(int64(len(deletions)))
	} else {
		for _, key := range deletions {
			if err := tr.DeleteStorage(s.address, key[:]); err != nil {
				s.db.setError(err)
				return nil, err
			}
			s.db.StorageDeleted.Add(1)
		}
	}
	if s.db.prefetcher != nil {
		s.db.prefetcher.used(s.addrHash, s.data.Root, nil, used)
	}
	s.uncommittedStorage = make(Storage) // empties the commit markers
	return tr, nil
}
