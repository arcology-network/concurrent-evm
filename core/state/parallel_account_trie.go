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
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/trie"
)

type accountTrieBatcher interface {
	UpdateAccountBatch([]trie.AccountUpdate) error
}

// applyAccountUpdatesInParallel applies account mutations through the optional
// MPT batch interface. It returns false without changing state when the active
// trie does not support batching or StateDB is configured as single-threaded.
func (s *StateDB) applyAccountUpdatesInParallel() ([]common.Address, bool) {
	batcher, ok := s.trie.(accountTrieBatcher)
	if !ok || s.singlethreaded {
		return nil, false
	}

	var (
		usedAddrs      []common.Address
		deletedAddrs   []common.Address
		updatedObjects []*stateObject
		accountUpdates []trie.AccountUpdate
	)

	for addr, operation := range s.mutations {
		if operation.applied {
			continue
		}

		operation.applied = true
		usedAddrs = append(usedAddrs, addr)

		if operation.isDelete() {
			deletedAddrs = append(deletedAddrs, addr)
			continue
		}

		object := s.stateObjects[addr]
		accountUpdates = append(accountUpdates, trie.AccountUpdate{
			Address: addr,
			Account: &object.data,
		})
		updatedObjects = append(updatedObjects, object)
		s.AccountUpdated++
	}

	// Keep deletions after updates to avoid unnecessary branch collapses and
	// database node resolutions.
	for _, address := range deletedAddrs {
		accountUpdates = append(accountUpdates, trie.AccountUpdate{
			Address: address,
			Delete:  true,
		})
	}

	if err := batcher.UpdateAccountBatch(accountUpdates); err != nil {
		s.setError(fmt.Errorf("update account trie batch: %v", err))
	}

	for _, object := range updatedObjects {
		if object.dirtyCode {
			s.trie.UpdateContractCode(object.Address(), common.BytesToHash(object.CodeHash()), object.code)
		}
	}

	s.AccountDeleted += len(deletedAddrs)

	return usedAddrs, true
}
