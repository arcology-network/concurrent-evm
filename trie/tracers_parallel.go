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

// parallelTracer is a tracer that can be used concurrently. It is a wrapper of multiple
// tracers, each of which is responsible for a shard of the trie. This is a lock-free optimization
// to avoid the bottleneck of a single tracer.
type parallelTracer struct {
	tracers [17]*tracer
}

// newTracer initializes the parallelTracer for capturing trie changes.
func newParaTracer() tracerInterface {
	paraTracer := &parallelTracer{}
	for i := 0; i < len(paraTracer.tracers); i++ {
		paraTracer.tracers[i] = newTracer()
	}
	return paraTracer
}

// onRead tracks the newly loaded trie node and caches the rlp-encoded
// blob internally. Don't change the value outside of function since
// it's not deep-copied.
func (t *parallelTracer) onRead(path []byte, val []byte) {
	if len(path) > 0 {
		t.tracers[(path[0])].onRead(path, val)
		return
	}
	t.tracers[16].onRead(path, val)
}

// onInsert tracks the newly inserted trie node. If it's already
// in the deletion set (resurrected node), then just wipe it from
// the deletion set as it's "untouched".
func (t *parallelTracer) onInsert(path []byte) {
	if len(path) > 0 {
		t.tracers[path[0]].onInsert(path)
		return
	}
	t.tracers[16].onInsert(path)
}

// onDelete tracks the newly deleted trie node. If it's already
// in the addition set, then just wipe it from the addition set
// as it's untouched.
func (t *parallelTracer) onDelete(path []byte) {
	if len(path) > 0 {
		t.tracers[path[0]].onDelete(path)
		return
	}
	t.tracers[16].onDelete(path)
}

// reset clears the content tracked by parallelTracer.
func (t *parallelTracer) reset() {
	for i := 0; i < len(t.tracers); i++ {
		t.tracers[i].reset()
	}
}

// copy returns a deep copied parallelTracer instance.
func (t *parallelTracer) copy() tracerInterface {
	paraTracer := newParaTracer().(*parallelTracer) //.(*parallelTracer)
	for i := 0; i < len(t.tracers); i++ {
		paraTracer.tracers[i] = t.tracers[i].copy() //.(*tracer)
	}
	return paraTracer
}

// markDeletions puts all tracked deletions into the provided nodeset.
// func (t *parallelTracer) markDeletions(set *trienode.NodeSet) {
// 	for i := 0; i < len(t.tracers); i++ {
// 		t.tracers[i].markDeletions(set)
// 	}
// }

// getAccessList returns the access list of the trie changes from allt he sub tracers.
func (t *parallelTracer) getAccessList() map[string][]byte {
	accessList := map[string][]byte{}
	for i := 0; i < len(t.tracers); i++ {
		for k, v := range t.tracers[i].accessList {
			accessList[k] = v
		}
	}
	return accessList
}

// getDeletes returns the deleted list of the trie changes from allt he sub tracers.
func (t *parallelTracer) getDeletes() map[string]struct{} {
	deletes := map[string]struct{}{}
	for i := 0; i < len(t.tracers); i++ {
		for k, v := range t.tracers[i].deletes {
			deletes[k] = v
		}
	}
	return deletes
}

// getInserts returns the inserted list of the trie changes from allt he sub tracers.
func (t *parallelTracer) getInserts() map[string]struct{} {
	inserts := map[string]struct{}{}
	for i := 0; i < len(t.tracers); i++ {
		for k, v := range t.tracers[i].inserts {
			inserts[k] = v
		}
	}
	return inserts
}

func (t *parallelTracer) deletedNodes() []string {
	var paths []string
	for i := 0; i < len(t.tracers); i++ {
		for path := range t.tracers[i].deletes {
			paths = append(paths, path)
		}
	}
	return paths
}
