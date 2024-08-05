/*
 *   Copyright (c) 2024 Arcology Network
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
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/triedb/database"
)

// func NewParallel(id *ID, db database.Database) (*Trie, error) {
// 	reader, err := newTrieReader(id.StateRoot, id.Owner, db)
// 	if reader == nil || err != nil {
// 		return nil, fmt.Errorf("state not found #%x", id.StateRoot)
// 	}

// 	trie, err := New(id, db)
// 	if err != nil {
// 		return nil, err
// 	}

// 	trie.tracer = newParaTracer()
// 	trie.reader = reader
// 	return trie, nil
// }

func NewParallel(id *ID, db database.Database) (*Trie, error) {
	trie, err := New(id, db)
	if err == nil {
		trie.tracer = newParaTracer()
	}
	return trie, err
}

func NewEmptyParallel(db database.Database) *Trie {
	trie := NewEmpty(db)
	trie.tracer = newParaTracer()
	return trie
}

func (t *Trie) threadSafeResolveAndTrack(n hashNode, prefix []byte, accesses *AccessListCache) (node, error) {
	blob, err := t.reader.node(prefix, common.BytesToHash(n))
	if err != nil {
		return nil, err
	}

	accesses.Add(prefix, blob)
	return mustDecodeNode(n, blob), nil
}

// wrong tracer !!! single only, should be multiple !!!
func (t *Trie) threadSafeUpdate(root node, key, value []byte) (node, error) {
	k := keybytesToHex(key)
	if len(value) != 0 {
		_, n, err := t.insert(root, nil, k, valueNode(value))
		if err != nil {
			return nil, err
		}

		return n, nil
	} else {
		_, n, err := t.delete(root, nil, k)
		if err != nil {
			return nil, err
		}
		return n, err
	}
}

func (t *Trie) ThreadSafeGet(key []byte, accesses *AccessListCache) ([]byte, error) {
	value, _, _, err := t.threadSafeGet(t.root, keybytesToHex(key), 0, accesses)
	return value, err
}

func (t *Trie) threadSafeGet(origNode node, key []byte, pos int, accesses *AccessListCache) (value []byte, newnode node, didResolve bool, err error) {
	switch n := (origNode).(type) {
	case nil:
		return nil, nil, false, nil
	case valueNode:
		return n, n, false, nil
	case *shortNode:
		if len(key)-pos < len(n.Key) || !bytes.Equal(n.Key, key[pos:pos+len(n.Key)]) {
			// key not found in trie
			return nil, n, false, nil
		}
		value, newnode, didResolve, err = t.threadSafeGet(n.Val, key, pos+len(n.Key), accesses)
		if err == nil && didResolve {
			n = n.copy()
			n.Val = newnode
		}
		return value, n, didResolve, err
	case *fullNode:
		value, newnode, didResolve, err = t.threadSafeGet(n.Children[key[pos]], key, pos+1, accesses)
		if err == nil && didResolve {
			// n = n.copy()
			// n.Children[key[pos]] = newnode
		}
		return value, n, didResolve, err
	case hashNode:
		child, err := t.threadSafeResolveAndTrack(n, key[:pos], accesses)
		if err != nil {
			return nil, n, true, err
		}
		value, newnode, _, err := t.threadSafeGet(child, key, pos, accesses)
		return value, newnode, true, err
	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

func (trie *Trie) initSubRoots(keys [][]byte, values [][]byte) bool {
	intialized := make([]bool, 16)
	for i := 0; i < len(keys); i++ {
		nibble := 0
		if len(keys[i]) > 0 {
			nibble = int(keys[i][0] >> 4)
		}

		if !intialized[nibble] {
			trie.Update(keys[i], values[i])
			intialized[nibble] = true
		}
	}
	_, ok := trie.root.(*shortNode)
	return ok
}

func (trie *Trie) ParallelUpdate(keys [][]byte, values [][]byte) []error {
	errs := make([]error, 16)

	if len(keys) == 0 || len(values) == 0 {
		return errs
	}

	// Initialize snapshots
	rootSnapshots := make([]node, 16)
	for start := 0; start < 16; start++ {
		rootSnapshots[start] = &fullNode{flags: trie.newFlag()}
	}

	if trie.initSubRoots(keys, values) {
		for i := 0; i < len(keys); i++ {
			trie.update(keys[i], values[i])
		}
		return errs
	}

	for i := 0; i < 16; i++ {
		if trie.root.(*fullNode).Children[i] != nil {
			rootSnapshots[i].(*fullNode).Children[i] = trie.root.(*fullNode).Children[i]
		}
	}

	inserters := func(start, end, index int, args ...interface{}) {
		// for start := 0; start < 16; start++ {
		for i := 0; i < len(keys); i++ {
			nibble := 0
			if len(keys[i]) > 0 {
				nibble = int(keys[i][0] >> 4)
			}

			if int(nibble) == start {
				if rootSnapshots[nibble] == nil {
					rootSnapshots[nibble] = trie.root
				}

				var err error
				if rootSnapshots[nibble], err = trie.threadSafeUpdate(rootSnapshots[nibble], keys[i], values[i]); err != nil {
					errs[nibble] = err
				}
			}
		}
	}
	ParallelWorker(16, 16, inserters)

	trie.unhashed = 1024 // To trigger parallel hasher
	for i := 0; i < 16; i++ {
		trie.root.(*fullNode).Children[i] = rootSnapshots[i].(*fullNode).Children[i]
	}
	return errs
}

func (trie *Trie) ParallelGet(keys [][]byte) ([][]byte, error) {
	values := make([][]byte, len(keys))
	if len(keys) == 0 {
		return values, nil
	}

	accesseCache := NewAccessListCaches(16)
	ParallelWorker(16, 16, func(start, end, index int, args ...interface{}) {
		for j := 0; j < len(keys); j++ {
			if nibble := (keys[j][0] >> 4); int(nibble) == start {
				values[j], _, _, _ = trie.threadSafeGet(trie.root, keybytesToHex(keys[j]), 0, accesseCache[nibble])
			}
		}
	})
	return values, nil
}
