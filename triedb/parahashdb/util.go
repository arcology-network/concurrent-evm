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

package hashdb

import (
	"math"
	"sync"
)

// The ParallelWorker function is a utility function that helps in parallelizing work across multiple threads or goroutines

func ParallelWorker(total, nThds int, worker func(start, end, idx int, args ...interface{}), args ...interface{}) {
	ranges := make([]int, 0, nThds+1)
	step := int(math.Ceil(float64(total) / float64(nThds)))
	for i := 0; i <= nThds; i++ {
		ranges = append(ranges, int(math.Min(float64(step*i), float64(nThds))))
	}

	var wg sync.WaitGroup
	for i := 0; i < len(ranges)-1; i++ {
		wg.Add(1)
		go func(start int, end int, idx int) {
			defer wg.Done()
			if start != end {
				worker(start, end, idx, args)
			}
		}(ranges[i], ranges[i+1], i)
	}
	wg.Wait()
}
