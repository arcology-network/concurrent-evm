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

import (
	"math"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func getShardIndexFromAddress(owner common.Address, numShard int) int {
	ownerHash := crypto.Keccak256Hash(owner.Bytes())
	return getShardIndexFromHash(ownerHash, numShard)
}

func getShardIndexFromHash(owner common.Hash, numShard int) int {
	if numShard <= 0 {
		panic("pathdb: shard count must be greater than zero")
	}
	return int(owner[0]) % numShard
}

func ParallelExecute(tasks ...func()) {
	ParallelWorker(len(tasks), len(tasks), func(start, end, _ int, _ ...any) {
		for i := start; i < end; i++ {
			tasks[i]()
		}
	})
}

func ParallelWorker(total, nThds int, worker func(start, end, idx int, args ...any), args ...any) {
	if total <= 0 || nThds <= 0 {
		return
	}

	nThds = min(total, nThds)
	stride := int(math.Ceil(float64(total) / float64(nThds)))

	var wg sync.WaitGroup
	for i := 0; i < nThds; i++ {
		end := int(min((i+1)*stride, total))
		wg.Add(1)
		go func(start int, end int, idx int) {
			defer wg.Done()
			worker(start, end, idx, args...)
		}(i*stride, end, i)
	}
	wg.Wait()
}
