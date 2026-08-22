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

package pathdb

import "github.com/ethereum/go-ethereum/common"

// regroup partitions flat-state changes and their origins using the same
// account-hash routing as paraReader.
func (set *StateSetWithOrigin) regroup(numShards int) []*StateSetWithOrigin {
	if numShards <= 0 {
		panic("pathdb: shard count must be greater than zero")
	}
	groups := make([]*StateSetWithOrigin, numShards)
	accounts := make([]map[common.Hash][]byte, numShards)
	storages := make([]map[common.Hash]map[common.Hash][]byte, numShards)
	accountOrigins := make([]map[common.Address][]byte, numShards)
	storageOrigins := make([]map[common.Address]map[common.Hash][]byte, numShards)
	for shard := 0; shard < numShards; shard++ {
		accounts[shard] = make(map[common.Hash][]byte)
		storages[shard] = make(map[common.Hash]map[common.Hash][]byte)
		accountOrigins[shard] = make(map[common.Address][]byte)
		storageOrigins[shard] = make(map[common.Address]map[common.Hash][]byte)
	}
	if set == nil {
		for shard := range groups {
			groups[shard] = NewStateSetWithOrigin(accounts[shard], storages[shard], accountOrigins[shard], storageOrigins[shard], false)
		}
		return groups
	}
	for hash, account := range set.accountData {
		shard := getShardIndexFromHash(hash, numShards)
		accounts[shard][hash] = account
	}
	for hash, slots := range set.storageData {
		shard := getShardIndexFromHash(hash, numShards)
		storages[shard][hash] = slots
	}
	for address, account := range set.accountOrigin {
		shard := getShardIndexFromAddress(address, numShards)
		accountOrigins[shard][address] = account
	}
	for address, slots := range set.storageOrigin {
		shard := getShardIndexFromAddress(address, numShards)
		storageOrigins[shard][address] = slots
	}
	for shard := range groups {
		groups[shard] = NewStateSetWithOrigin(accounts[shard], storages[shard], accountOrigins[shard], storageOrigins[shard], set.rawStorageKey)
	}
	return groups
}
