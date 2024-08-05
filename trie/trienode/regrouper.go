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

package trienode

func (set *MergedNodeSet) Regroup() ([]*MergedNodeSet, *MergedNodeSet, []bool) {
	regrouped := make([]*MergedNodeSet, 17)
	for i := range regrouped { // break into 16 shards
		regrouped[i] = NewMergedNodeSet()
		for owner := range set.Sets {
			regrouped[i].Sets[owner] = NewNodeSet(owner)
		}
	}

	shards := make([]bool, 16)
	for i := 0; i < len(regrouped); i++ {
		for owner, v := range set.Sets {
			for k, v := range v.Nodes {
				if len(k) > 0 {
					shards[k[0]] = true
					// fmt.Println(k[0])
					regrouped[k[0]].Sets[owner].Nodes[k] = v
				} else {
					// fmt.Println(k)
					regrouped[16].Sets[owner].Nodes[k] = v
				}
			}
		}
	}
	return regrouped[0:16], regrouped[16], shards
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
