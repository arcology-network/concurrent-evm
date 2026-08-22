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

package trie

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// AccountUpdate describes an account-trie mutation. Account is ignored when
// Delete is set.
type AccountUpdate struct {
	Address common.Address
	Account *types.StateAccount
	Delete  bool
}

// UpdateAccountBatch encodes and applies a collection of account mutations.
// The underlying MPT partitions sufficiently large batches across independent
// root branches.
func (t *StateTrie) UpdateAccountBatch(updates []AccountUpdate) error {
	operations := make([]UpdateOperation, 0, len(updates))
	keys := make([][]byte, 0, len(updates))

	for _, update := range updates {
		key := crypto.Keccak256(update.Address.Bytes())
		var value []byte

		if !update.Delete {
			encoded, err := rlp.EncodeToBytes(update.Account)
			if err != nil {
				return err
			}
			value = encoded
		}

		operations = append(operations, UpdateOperation{Key: key, Value: value})
		keys = append(keys, key)
	}

	if err := t.trie.UpdateBatch(operations); err != nil {
		return err
	}

	if t.preimages != nil {
		for i, update := range updates {
			key := common.BytesToHash(keys[i])

			if update.Delete {
				delete(t.secKeyCache, key)
			} else {
				t.secKeyCache[key] = update.Address.Bytes()
			}
		}
	}

	return nil
}
