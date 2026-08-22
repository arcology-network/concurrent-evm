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

// pristine reports whether the tracer has no local or attached changes.
func (t *opTracer) pristine() bool {
	if len(t.inserts) != 0 || len(t.deletes) != 0 {
		return false
	}
	for _, shard := range t.shards {
		if shard != nil {
			return false
		}
	}
	return true
}

// attachShard retains a root-subtree tracer without copying its path maps.
func (t *opTracer) attachShard(position int, shard *opTracer) {
	t.shards[position] = shard
}

// shard returns the attached tracer which owns path, if one exists.
func (t *opTracer) shard(path []byte) *opTracer {
	if len(path) == 0 || path[0] >= parallelTrieShardCount {
		return nil
	}
	return t.shards[path[0]]
}

func (t *opTracer) deleteCount() int {
	count := len(t.deletes)
	for _, shard := range t.shards {
		if shard != nil {
			count += shard.deleteCount()
		}
	}
	return count
}

func (t *opTracer) appendDeleted(paths *[][]byte) {
	for path := range t.deletes {
		*paths = append(*paths, []byte(path))
	}
	for _, shard := range t.shards {
		if shard != nil {
			shard.appendDeleted(paths)
		}
	}
}
