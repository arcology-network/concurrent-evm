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
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestParallelStorageTrieUpdatesMatchSequential(t *testing.T) {
	sequential, err := New(types.EmptyRootHash, NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("failed to create sequential state: %v", err)
	}
	sequential.MakeSinglethreaded()

	parallel, err := New(types.EmptyRootHash, NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("failed to create parallel state: %v", err)
	}

	address := common.HexToAddress("01")
	slot := func(index int) common.Hash {
		var key common.Hash
		binary.BigEndian.PutUint64(key[24:], uint64(index))
		return key
	}
	value := func(index, generation int) common.Hash {
		var value common.Hash
		binary.BigEndian.PutUint64(value[24:], uint64(index+1+generation*1024))
		return value
	}

	// The first phase constructs a populated storage trie. UpdateBatch
	// transparently uses its sequential fallback because the trie is new.
	for i := 0; i < 256; i++ {
		sequential.SetState(address, slot(i), value(i, 0))
		parallel.SetState(address, slot(i), value(i, 0))
	}
	if have, want := parallel.IntermediateRoot(false), sequential.IntermediateRoot(false); have != want {
		t.Fatalf("initial state root mismatch: have %x want %x", have, want)
	}

	// The second phase exercises mixed updates, deletions, and inserts against
	// the populated trie. The parallel StateDB uses UpdateStorageBatch while the
	// single-threaded StateDB remains the independent sequential oracle.
	for i := 0; i < 256; i++ {
		var next common.Hash
		if i%3 != 0 {
			next = value(i, 1)
		}
		sequential.SetState(address, slot(i), next)
		parallel.SetState(address, slot(i), next)
	}
	for i := 256; i < 512; i++ {
		sequential.SetState(address, slot(i), value(i, 1))
		parallel.SetState(address, slot(i), value(i, 1))
	}
	if have, want := parallel.IntermediateRoot(false), sequential.IntermediateRoot(false); have != want {
		t.Fatalf("updated state root mismatch: have %x want %x", have, want)
	}

	for i := 0; i < 512; i++ {
		if have, want := parallel.GetState(address, slot(i)), sequential.GetState(address, slot(i)); have != want {
			t.Fatalf("storage mismatch at slot %d: have %x want %x", i, have, want)
		}
	}
}
