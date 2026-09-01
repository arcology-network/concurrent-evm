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
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

func TestUpdateBatchEquivalent(t *testing.T) {
	db, root := makeBatchTestTrie(t, 256)
	sequential := openBatchTestTrie(t, db, root)
	parallel := openBatchTestTrie(t, db, root)

	var operations []UpdateOperation
	for i := 0; i < 64; i++ {
		operations = append(operations, UpdateOperation{
			Key:   batchTestKey(i, 0),
			Value: bytes.Repeat([]byte{byte(i + 1)}, 48),
		})
	}

	for i := 64; i < 128; i++ {
		operations = append(operations, UpdateOperation{Key: batchTestKey(i, 0)})
	}

	for i := 128; i < 192; i++ {
		operations = append(operations, UpdateOperation{
			Key:   batchTestKey(i, 1),
			Value: bytes.Repeat([]byte{byte(i + 1)}, 48),
		})
	}

	applySequentially(t, sequential, operations)

	if err := parallel.UpdateBatch(operations); err != nil {
		t.Fatalf("parallel update failed: %v", err)
	}

	assertBatchTriesEqual(t, sequential, parallel)
}

func TestUpdateBatchTrieShapes(t *testing.T) {
	allShards := make([]int, parallelTrieShardCount)
	for i := range allShards {
		allShards[i] = i
	}

	tests := []struct {
		name          string
		initialShards []int
		wantParallel  bool
		checkRoot     func(*testing.T, node)
	}{
		{
			name:         "new trie",
			wantParallel: false,
			checkRoot: func(t *testing.T, root node) {
				t.Helper()
				if root != nil {
					t.Fatalf("new trie root is not nil: %T", root)
				}
			},
		},
		{
			name:          "complete trie",
			initialShards: allShards,
			wantParallel:  true,
			checkRoot: func(t *testing.T, root node) {
				t.Helper()
				branch, ok := root.(*fullNode)
				if !ok {
					t.Fatalf("complete trie root is not a full node: %T", root)
				}
				for position := 0; position < parallelTrieShardCount; position++ {
					if branch.Children[position] == nil {
						t.Fatalf("complete trie is missing root shard %d", position)
					}
				}
			},
		},
		{
			name:          "incomplete trie",
			initialShards: []int{0, 4, 8, 12},
			wantParallel:  true,
			checkRoot: func(t *testing.T, root node) {
				t.Helper()
				branch, ok := root.(*fullNode)
				if !ok {
					t.Fatalf("incomplete trie root is not a full node: %T", root)
				}
				if branch.Children[0] == nil || branch.Children[4] == nil || branch.Children[8] == nil || branch.Children[12] == nil {
					t.Fatal("incomplete trie is missing an expected existing root shard")
				}
				if branch.Children[1] != nil || branch.Children[15] != nil {
					t.Fatal("incomplete trie unexpectedly contains every root shard")
				}
			},
		},
	}

	operations := make([]UpdateOperation, 0, 2*parallelTrieShardCount)
	for i := 0; i < 2*parallelTrieShardCount; i++ {
		position := i % parallelTrieShardCount
		operations = append(operations, UpdateOperation{
			Key:   batchTestKey((position<<4)+(i/parallelTrieShardCount)+1, 1),
			Value: bytes.Repeat([]byte{byte(i + 1)}, 48),
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sequential := NewEmpty(nil)
			parallel := NewEmpty(nil)

			for _, position := range test.initialShards {
				operation := UpdateOperation{
					Key:   batchTestKey(position<<4, 0),
					Value: []byte{byte(position + 1)},
				}
				if err := sequential.Update(operation.Key, operation.Value); err != nil {
					t.Fatalf("failed to initialize sequential trie: %v", err)
				}
				if err := parallel.Update(operation.Key, operation.Value); err != nil {
					t.Fatalf("failed to initialize batch trie: %v", err)
				}
			}

			test.checkRoot(t, parallel.root)
			_, _, parallelPath := parallel.prepareUpdateBuckets(operations)
			if parallelPath != test.wantParallel {
				t.Fatalf("parallel path = %t, want %t", parallelPath, test.wantParallel)
			}

			applySequentially(t, sequential, operations)
			if err := parallel.UpdateBatch(operations); err != nil {
				t.Fatalf("batch update failed: %v", err)
			}

			assertBatchTriesEqual(t, sequential, parallel)
		})
	}
}

func TestUpdateBatchRootReduction(t *testing.T) {
	t.Run("single child", func(t *testing.T) {
		db, root := makeBatchTestTrie(t, parallelTrieShardCount)
		sequential := openBatchTestTrie(t, db, root)
		parallel := openBatchTestTrie(t, db, root)

		operations := make([]UpdateOperation, 0, parallelTrieShardCount)
		for i := 0; i < 15; i++ {
			operations = append(operations, UpdateOperation{Key: batchTestKey(i<<4, 0)})
		}

		operations = append(operations, UpdateOperation{
			Key:   batchTestKey(15<<4, 0),
			Value: []byte("updated survivor"),
		})

		applySequentially(t, sequential, operations)

		if err := parallel.UpdateBatch(operations); err != nil {
			t.Fatalf("parallel update failed: %v", err)
		}

		if _, ok := parallel.root.(*shortNode); !ok {
			t.Fatalf("root was not reduced to a short node: %T", parallel.root)
		}

		assertBatchTriesEqual(t, sequential, parallel)
	})

	t.Run("empty", func(t *testing.T) {
		db, root := makeBatchTestTrie(t, parallelTrieShardCount)
		sequential := openBatchTestTrie(t, db, root)
		parallel := openBatchTestTrie(t, db, root)

		operations := make([]UpdateOperation, 0, parallelTrieShardCount)
		for i := 0; i < parallelTrieShardCount; i++ {
			operations = append(operations, UpdateOperation{Key: batchTestKey(i<<4, 0)})
		}

		applySequentially(t, sequential, operations)

		if err := parallel.UpdateBatch(operations); err != nil {
			t.Fatalf("parallel update failed: %v", err)
		}

		if parallel.root != nil {
			t.Fatalf("root was not reduced to nil: %T", parallel.root)
		}

		assertBatchTriesEqual(t, sequential, parallel)
	})
}

func TestUpdateBatchSequentialFallback(t *testing.T) {
	sequential := NewEmpty(nil)
	batched := NewEmpty(nil)
	operations := make([]UpdateOperation, 0, 64)

	for i := 0; i < 64; i++ {
		operations = append(operations, UpdateOperation{
			Key:   batchTestKey(i, 0),
			Value: []byte{byte(i + 1)},
		})
	}

	applySequentially(t, sequential, operations)

	if err := batched.UpdateBatch(operations); err != nil {
		t.Fatalf("batch fallback failed: %v", err)
	}

	assertBatchTriesEqual(t, sequential, batched)
}

func TestUpdateBatchPreservesSameKeyOrder(t *testing.T) {
	db, root := makeBatchTestTrie(t, 256)
	sequential := openBatchTestTrie(t, db, root)
	parallel := openBatchTestTrie(t, db, root)
	target := batchTestKey(1, 0)

	operations := []UpdateOperation{
		{Key: target, Value: []byte("first")},
		{Key: target},
		{Key: target, Value: []byte("last")},
	}

	for i := parallelTrieShardCount; i < 2*parallelTrieShardCount; i++ {
		operations = append(operations, UpdateOperation{
			Key:   batchTestKey(i, 0),
			Value: []byte{byte(i)},
		})
	}

	applySequentially(t, sequential, operations)

	if err := parallel.UpdateBatch(operations); err != nil {
		t.Fatalf("parallel update failed: %v", err)
	}

	if value, err := parallel.Get(target); err != nil || !bytes.Equal(value, []byte("last")) {
		t.Fatalf("last operation did not win: value %q error %v", value, err)
	}

	assertBatchTriesEqual(t, sequential, parallel)
}

func TestUpdateBatchAfterPreviousBatch(t *testing.T) {
	db, root := makeBatchTestTrie(t, 256)
	sequential := openBatchTestTrie(t, db, root)
	parallel := openBatchTestTrie(t, db, root)

	first := make([]UpdateOperation, 0, 64)
	for i := 0; i < 64; i++ {
		first = append(first, UpdateOperation{
			Key:   batchTestKey(i, 0),
			Value: []byte{byte(i + 1), 0xaa},
		})
	}
	applySequentially(t, sequential, first)
	if err := parallel.UpdateBatch(first); err != nil {
		t.Fatalf("first parallel update failed: %v", err)
	}

	second := make([]UpdateOperation, 0, 64)
	for i := 0; i < 32; i++ {
		second = append(second, UpdateOperation{Key: batchTestKey(i, 0)})
	}
	for i := 32; i < 64; i++ {
		second = append(second, UpdateOperation{
			Key:   batchTestKey(i, 0),
			Value: []byte{byte(i + 1), 0xbb},
		})
	}
	applySequentially(t, sequential, second)
	if err := parallel.UpdateBatch(second); err != nil {
		t.Fatalf("second parallel update failed: %v", err)
	}

	key := batchTestKey(1, 0)
	value := []byte("recreated after batches")
	if err := sequential.Update(key, value); err != nil {
		t.Fatalf("sequential final update failed: %v", err)
	}
	if err := parallel.Update(key, value); err != nil {
		t.Fatalf("parallel final update failed: %v", err)
	}

	assertBatchTriesEqual(t, sequential, parallel)
}

func TestStateTrieUpdateAccountBatch(t *testing.T) {
	sequential, err := NewStateTrie(StateTrieID(types.EmptyRootHash), newTestDatabase(rawdb.NewMemoryDatabase(), rawdb.HashScheme))
	if err != nil {
		t.Fatalf("failed to create sequential state trie: %v", err)
	}

	parallel, err := NewStateTrie(StateTrieID(types.EmptyRootHash), newTestDatabase(rawdb.NewMemoryDatabase(), rawdb.HashScheme))
	if err != nil {
		t.Fatalf("failed to create parallel state trie: %v", err)
	}

	addresses := make([]common.Address, 128)

	for i := range addresses {
		binary.BigEndian.PutUint64(addresses[i][12:], uint64(i+1))
		account := types.NewEmptyStateAccount()
		account.Nonce = uint64(i)
		if err := sequential.UpdateAccount(addresses[i], account, 0); err != nil {
			t.Fatalf("failed to initialize sequential account: %v", err)
		}
		if err := parallel.UpdateAccount(addresses[i], account, 0); err != nil {
			t.Fatalf("failed to initialize parallel account: %v", err)
		}
	}

	if _, ok := parallel.trie.root.(*fullNode); !ok {
		t.Fatalf("state trie root is not a full node: %T", parallel.trie.root)
	}

	updates := make([]AccountUpdate, 0, len(addresses))

	for i, address := range addresses {
		if i%3 == 0 {
			updates = append(updates, AccountUpdate{Address: address, Delete: true})
			if err := sequential.DeleteAccount(address); err != nil {
				t.Fatalf("failed to delete sequential account: %v", err)
			}
			continue
		}
		account := types.NewEmptyStateAccount()
		account.Nonce = uint64(i + 1000)

		updates = append(updates, AccountUpdate{Address: address, Account: account})
		if err := sequential.UpdateAccount(address, account, 0); err != nil {
			t.Fatalf("failed to update sequential account: %v", err)
		}
	}

	if err := parallel.UpdateAccountBatch(updates); err != nil {
		t.Fatalf("failed to update account batch: %v", err)
	}

	if have, want := parallel.Hash(), sequential.Hash(); have != want {
		t.Fatalf("state root mismatch: have %x want %x", have, want)
	}

	haveRoot, haveNodes := parallel.Commit(false)
	wantRoot, wantNodes := sequential.Commit(false)

	if haveRoot != wantRoot {
		t.Fatalf("state commit root mismatch: have %x want %x", haveRoot, wantRoot)
	}

	if have, want := printSet(haveNodes), printSet(wantNodes); have != want {
		t.Fatalf("state nodeset mismatch\nhave:\n%s\nwant:\n%s", have, want)
	}
}

func BenchmarkUpdateBatchComparison(b *testing.B) {
	db, root := makeBatchTestTrie(b, 8192)

	for _, count := range []int{64, 256, 1024, 4096} {
		operations := makeBenchmarkOperations(count, 8192)

		b.Run(fmt.Sprintf("operations=%d/sequential", count), func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				b.StopTimer()
				tr := openBatchTestTrie(b, db, root)
				b.StartTimer()

				for _, operation := range operations {
					if err := tr.Update(operation.Key, operation.Value); err != nil {
						b.Fatalf("sequential update failed: %v", err)
					}
				}
			}
		})

		b.Run(fmt.Sprintf("operations=%d/parallel", count), func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				b.StopTimer()
				tr := openBatchTestTrie(b, db, root)
				b.StartTimer()

				if err := tr.UpdateBatch(operations); err != nil {
					b.Fatalf("parallel update failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkMillionUpdateComparison(b *testing.B) {
	db, root := makeBatchTestTrie(b, 8192)
	value := bytes.Repeat([]byte{0x7f}, 48)
	operations := make([]UpdateOperation, 1_000_000)

	for i := range operations {
		operations[i] = UpdateOperation{
			Key:   batchTestKey(i, 1),
			Value: value,
		}
	}

	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			b.StopTimer()
			tr := openBatchTestTrie(b, db, root)
			b.StartTimer()

			for _, operation := range operations {
				if err := tr.Update(operation.Key, operation.Value); err != nil {
					b.Fatalf("sequential update failed: %v", err)
				}
			}
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			b.StopTimer()
			tr := openBatchTestTrie(b, db, root)
			b.StartTimer()

			if err := tr.UpdateBatch(operations); err != nil {
				b.Fatalf("parallel update failed: %v", err)
			}
		}
	})
}

func makeBenchmarkOperations(count, baseSize int) []UpdateOperation {
	updates := count * 3 / 4
	operations := make([]UpdateOperation, 0, count)

	for i := 0; i < updates; i++ {
		operations = append(operations, UpdateOperation{
			Key:   batchTestKey(i, 0),
			Value: bytes.Repeat([]byte{byte(i + 1)}, 48),
		})
	}

	for i := 0; i < count-updates; i++ {
		operations = append(operations, UpdateOperation{
			Key: batchTestKey(baseSize-1-i, 0),
		})
	}

	return operations
}

func makeBatchTestTrie(t testing.TB, count int) (*testDb, common.Hash) {
	t.Helper()

	db := newTestDatabase(rawdb.NewMemoryDatabase(), rawdb.HashScheme)
	tr := NewEmpty(db)

	for i := 0; i < count; i++ {
		index := i

		if count == parallelTrieShardCount {
			index = i << 4
		}

		if err := tr.Update(batchTestKey(index, 0), []byte{byte(i + 1)}); err != nil {
			t.Fatalf("failed to construct trie: %v", err)
		}
	}

	root, nodes := tr.Commit(false)

	if err := db.Update(root, types.EmptyRootHash, trienode.NewWithNodeSet(nodes)); err != nil {
		t.Fatalf("failed to store trie: %v", err)
	}

	return db, root
}

func openBatchTestTrie(t testing.TB, db *testDb, root common.Hash) *Trie {
	t.Helper()

	tr, err := New(TrieID(root), db)

	if err != nil {
		t.Fatalf("failed to open trie: %v", err)
	}

	if _, ok := tr.root.(*fullNode); !ok {
		t.Fatalf("test trie root is not a full node: %T", tr.root)
	}

	return tr
}

func batchTestKey(index, generation int) []byte {
	key := make([]byte, 32)
	key[0] = byte(index)
	key[1] = byte(generation)
	binary.BigEndian.PutUint64(key[24:], uint64(index))

	return key
}

func applySequentially(t *testing.T, tr *Trie, operations []UpdateOperation) {
	t.Helper()

	for _, operation := range operations {
		if err := tr.Update(operation.Key, operation.Value); err != nil {
			t.Fatalf("sequential update failed: %v", err)
		}
	}
}

func assertBatchTriesEqual(t *testing.T, sequential, parallel *Trie) {
	t.Helper()

	wantRoot := sequential.Hash()
	haveRoot := parallel.Hash()

	if haveRoot != wantRoot {
		t.Fatalf("root mismatch: have %x want %x", haveRoot, wantRoot)
	}

	wantRoot, wantNodes := sequential.Commit(false)
	haveRoot, haveNodes := parallel.Commit(false)

	if haveRoot != wantRoot {
		t.Fatalf("commit root mismatch: have %x want %x", haveRoot, wantRoot)
	}

	want := printSet(wantNodes)
	have := printSet(haveNodes)

	if have != want {
		t.Fatalf("nodeset mismatch\nhave:\n%s\nwant:\n%s", have, want)
	}

	if !reflect.DeepEqual(haveNodes.Nodes, wantNodes.Nodes) {
		t.Fatal("committed node blobs differ")
	}

	if !reflect.DeepEqual(haveNodes.Origins, wantNodes.Origins) {
		t.Fatal("committed node origins differ")
	}
}
