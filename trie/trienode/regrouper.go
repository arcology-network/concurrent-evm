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

package trienode

import "github.com/ethereum/go-ethereum/common"

// Regroup partitions storage-trie nodes across data shards and returns
// account-trie nodes separately for the standalone account-trie database.
func (set *MergedNodeSet) Regroup(numShards int) ([]*MergedNodeSet, *MergedNodeSet) {
	if numShards <= 0 {
		panic("trienode: shard count must be greater than zero")
	}
	regrouped := make([]*MergedNodeSet, numShards)
	for shard := range regrouped {
		regrouped[shard] = NewMergedNodeSet()
	}
	accountTrie := NewMergedNodeSet()
	if set == nil {
		return regrouped, accountTrie
	}
	for owner, nodes := range set.Sets {
		if owner == (common.Hash{}) {
			accountTrie.Sets[owner] = nodes
			continue
		}
		shard := int(owner[0]) % numShards
		regrouped[shard].Sets[owner] = nodes
	}
	return regrouped, accountTrie
}

type MergedNodeSets []*MergedNodeSet

func (nodeset MergedNodeSets) Count() int {
	total := 0
	for i := 0; i < len(nodeset); i++ {
		for _, v := range nodeset[i].Sets {
			total += len(v.Nodes)
		}
	}
	return total
}
