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

type AccessListCache struct {
	// tx   uint32
	keys [][]byte
	data [][]byte
}

func NewAccessListCaches(num int) []*AccessListCache {
	list := make([]*AccessListCache, num)
	for i := 0; i < len(list); i++ {
		list[i] = &AccessListCache{
			keys: [][]byte{},
			data: [][]byte{},
		}
	}
	return list
}

func (this *AccessListCache) Add(key []byte, val []byte) {
	this.keys = append(this.keys, key)
	this.data = append(this.data, val)
}

func (this *AccessListCache) Merge(accesses ...*AccessListCache) {
	for _, v := range accesses {
		this.keys = append(this.keys, v.keys...)
		this.data = append(this.data, v.data...)
	}
}

func (this *AccessListCache) ToMap() map[string][]byte {
	hashmap := map[string][]byte{}
	for i, k := range this.keys {
		hashmap[string(k)] = this.data[i]
	}
	return hashmap
}

func (this *AccessListCache) Unique() ([]string, [][]byte) {
	hashmap := this.ToMap()
	keys, values := make([]string, 0, len(hashmap)), make([][]byte, 0, len(hashmap))
	for k, v := range hashmap {
		keys = append(keys, k)
		values = append(values, v)
	}
	return keys, values
}
