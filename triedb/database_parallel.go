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

package triedb

import (
	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie"
	parahashdb "github.com/ethereum/go-ethereum/triedb/parahashdb"
)

// NewParallelDatabase creates a parallel database in place of the original triedb.Database
func NewParallelDatabase(diskdbs [16]ethdb.Database, config *Config) *Database {
	dbs := NewDatabase(diskdbs[0], config) // For preimage

	dbConfig := &parahashdb.Config{CleanCacheSize: 1024 * 1024 * 10}
	dbs.backend = parahashdb.New(diskdbs, trie.MerkleResolver{}, dbConfig)
	return dbs
}

// NewParallelDatabaseWithSharedCache creates a parallel database with shared cache
func NewParallelDatabaseWithSharedCache(diskdbs [16]ethdb.Database, cleanCache *fastcache.Cache, config *Config) *Database {
	dbs := NewDatabase(diskdbs[0], config) // For preimage
	dbs.backend = parahashdb.NewWithCache(diskdbs, config, trie.MerkleResolver{}, cleanCache, nil)
	return dbs
}

// GetBackendDB returns the backend database of the parallel database
func GetBackendDB(this *Database) *parahashdb.Database {
	if db, ok := this.backend.(*parahashdb.Database); ok {
		return db
	}
	return nil
}
