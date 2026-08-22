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
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// New HistoricalStateReader implements a wrapper over the original history reader,
// providing access to historical state.
type HistoricalStateReader struct {
	stateReaders []*historicalStateReader
}

func (r *HistoricalStateReader) AccountRLP(address common.Address) ([]byte, error) {
	shard := getShardIndexFromAddress(address, len(r.stateReaders))
	return r.stateReaders[shard].accountRLP(address)
}

func (r *HistoricalStateReader) Account(address common.Address) (*types.SlimAccount, error) {
	shard := getShardIndexFromAddress(address, len(r.stateReaders))
	return r.stateReaders[shard].account(address)
}

func (r *HistoricalStateReader) Storage(address common.Address, key common.Hash) ([]byte, error) {
	shard := getShardIndexFromAddress(address, len(r.stateReaders))
	return r.stateReaders[shard].storage(address, key)
}

// HistoricReader constructs one historical reader per shard. All shards must
// contain the requested canonical state before the aggregate reader is usable.
func (db *ParaDatabase) HistoricReader(root common.Hash) (*HistoricalStateReader, error) {
	readers := make([]*historicalStateReader, len(db.databases))
	for shard, database := range db.databases {
		reader, err := database.historicReader(root)
		if err != nil {
			return nil, fmt.Errorf("open historical state on shard %d: %w", shard, err)
		}
		readers[shard] = reader
	}
	return &HistoricalStateReader{stateReaders: readers}, nil
}
