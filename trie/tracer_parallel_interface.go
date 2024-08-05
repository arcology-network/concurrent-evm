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

// An interface for trie tracers to replace the concrete tracer type to allow
// concurrent operations.
type tracerInterface interface {
	onRead([]byte, []byte)
	onInsert([]byte)
	onDelete(path []byte)
	reset()
	copy() tracerInterface
	// markDeletions(set *trienode.NodeSet)
	getAccessList() map[string][]byte
	getDeletes() map[string]struct{}
	getInserts() map[string]struct{}
	deletedNodes() []string
}
