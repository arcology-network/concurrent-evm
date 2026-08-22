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

import "golang.org/x/sync/errgroup"

const (
	parallelTrieShardCount  = 16
	parallelUpdateThreshold = parallelTrieShardCount
)

// UpdateOperation describes a single trie mutation. A zero-length value deletes
// the key. Operations which address the same key are applied in input order.
type UpdateOperation struct {
	Key   []byte
	Value []byte
}

// batchResult contains the state owned by a single root-subtree worker.
type batchResult struct {
	root      node
	dirty     bool
	attempted int
	tracer    *opTracer
}

// UpdateBatch applies a collection of trie mutations. If the trie root is a
// resolved branch and the batch is large enough, operations are partitioned by
// their first key nibble and applied to independent root subtries concurrently.
// Small batches and tries which cannot be partitioned safely are updated
// sequentially.
//
// The key and value bytes must not be modified by the caller while they are
// stored in the trie.
func (t *Trie) UpdateBatch(operations []UpdateOperation) error {
	if t.committed {
		return ErrCommitted
	}

	if len(operations) == 0 {
		return nil
	}

	root, buckets, parallel := t.prepareUpdateBuckets(operations)
	if !parallel {
		return t.updateSequentially(operations)
	}

	results, batchErr := t.executeUpdateBuckets(root, operations, buckets)

	return t.mergeBatchResults(root, results, batchErr)
}

// prepareUpdateBuckets validates that root-level parallelism is useful and
// partitions operations by the root child they modify.
func (t *Trie) prepareUpdateBuckets(operations []UpdateOperation) (*fullNode, [parallelTrieShardCount][]int, bool) {
	var (
		buckets [parallelTrieShardCount][]int
		counts  [parallelTrieShardCount]int
		offsets [parallelTrieShardCount]int
	)

	root, ok := t.root.(*fullNode)
	if !ok || len(operations) < parallelUpdateThreshold {
		return nil, buckets, false
	}

	// Count operations per root child before allocating the buckets. The first
	// nibble is the high half of the first key byte, so no nibble-path
	// allocation is needed during this pass.
	active := 0
	for _, operation := range operations {
		if len(operation.Key) == 0 {
			return nil, buckets, false
		}

		position := operation.Key[0] >> 4
		if counts[position] == 0 {
			active++
		}
		counts[position]++
	}

	if active < 2 {
		return nil, buckets, false
	}

	// Allocate every bucket at its exact final length.
	for position, count := range counts {
		if count != 0 {
			buckets[position] = make([]int, count)
		}
	}

	// Fill the preallocated buckets with operation indices. Workers convert the
	// keys to nibble paths concurrently.
	for operationIndex, operation := range operations {
		position := operation.Key[0] >> 4
		bucketIndex := offsets[position]

		buckets[position][bucketIndex] = operationIndex
		offsets[position]++
	}

	return root, buckets, true
}

// executeUpdateBuckets processes every non-empty root bucket in a separate
// worker and waits for all workers to finish.
func (t *Trie) executeUpdateBuckets(root *fullNode, operations []UpdateOperation, buckets [parallelTrieShardCount][]int) ([parallelTrieShardCount]batchResult, error) {
	var (
		results [parallelTrieShardCount]batchResult
		group   errgroup.Group
	)

	for position, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}

		group.Go(func() error {
			result, err := t.updateBucket(root.Children[position], byte(position), operations, bucket)
			results[position] = result

			return err
		})
	}

	return results, group.Wait()
}

// updateBucket applies operations sequentially within one exclusively owned
// root subtree. The worker uses a private opTracer for mutation bookkeeping.
func (t *Trie) updateBucket(child node, position byte, operations []UpdateOperation, indices []int) (batchResult, error) {
	worker := &Trie{
		owner:          t.owner,
		reader:         t.reader,
		opTracer:       newOpTracer(),
		prevalueTracer: t.prevalueTracer,
	}
	result := batchResult{
		root:   child,
		tracer: worker.opTracer,
	}
	prefix := []byte{position}

	for _, index := range indices {
		operation := operations[index]
		key := keybytesToHex(operation.Key)

		result.attempted++

		var (
			dirty bool
			next  node
			err   error
		)

		if len(operation.Value) == 0 {
			dirty, next, err = worker.delete(child, prefix, key[1:])
		} else {
			dirty, next, err = worker.insert(child, prefix, key[1:], valueNode(operation.Value))
		}

		if err != nil {
			return result, err
		}

		if dirty {
			child = next
			result.root = child
			result.dirty = true
		}
	}

	return result, nil
}

// mergeBatchResults reattaches worker-owned subtrees and merges their counters
// and tracers into the parent trie.
func (t *Trie) mergeBatchResults(root *fullNode, results [parallelTrieShardCount]batchResult, batchErr error) error {
	var (
		dirty        bool
		attachShards = t.opTracer.pristine()
	)

	for position, result := range results {
		t.unhashed += result.attempted
		t.uncommitted += result.attempted

		if !result.dirty {
			continue
		}

		dirty = true
		root.Children[position] = result.root
		if attachShards {
			t.opTracer.attachShard(position, result.tracer)
		} else {
			t.opTracer.merge(result.tracer)
		}
	}

	if !dirty {
		return batchErr
	}

	root.flags = t.newFlag()
	reduced, err := t.reduceRoot(root)
	if err != nil {
		return err
	}

	t.root = reduced

	return batchErr
}

func (t *Trie) updateSequentially(operations []UpdateOperation) error {
	for _, operation := range operations {
		if err := t.update(operation.Key, operation.Value); err != nil {
			return err
		}
	}

	return nil
}

// reduceRoot restores the canonical trie shape after several root children
// have been mutated independently. Unlike delete's full-node reduction, a
// batch can remove every child at once.
func (t *Trie) reduceRoot(root *fullNode) (node, error) {
	position := -1

	for i, child := range &root.Children {
		if child == nil {
			continue
		}

		if position != -1 {
			return root, nil
		}

		position = i
	}

	if position == -1 {
		t.opTracer.onDelete(nil)

		return nil, nil
	}

	if position != parallelTrieShardCount {
		child, err := t.resolve(root.Children[position], []byte{byte(position)})
		if err != nil {
			return nil, err
		}

		if child, ok := child.(*shortNode); ok {
			t.opTracer.onDelete([]byte{byte(position)})
			key := append([]byte{byte(position)}, child.Key...)

			return &shortNode{key, child.Val, t.newFlag()}, nil
		}
	}

	return &shortNode{[]byte{byte(position)}, root.Children[position], t.newFlag()}, nil
}

// merge incorporates the net changes collected by another tracer.
func (t *opTracer) merge(other *opTracer) {
	for path := range other.inserts {
		t.onInsert([]byte(path))
	}

	for path := range other.deletes {
		t.onDelete([]byte(path))
	}

	for _, shard := range other.shards {
		if shard != nil {
			t.merge(shard)
		}
	}
}
