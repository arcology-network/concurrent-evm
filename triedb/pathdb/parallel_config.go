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
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

const defaultHandles = 1024

// Config contains the parallel database configuration loaded from TOML.
type ParaConfig struct {
	ParaDatabase ParaDatabaseConfig `toml:"para-database"`
}

// ParaDatabaseConfig contains settings shared by the data shards and account
// trie database. NumShards counts only the numbered data shards.
type ParaDatabaseConfig struct {
	Engine    string `toml:"engine"`
	NumShards int    `toml:"num-shards"`
	Path      string `toml:"path"`
	CacheMB   int    `toml:"cache-mb"`
	Handles   int    `toml:"handles"`
	ReadOnly  bool   `toml:"read-only"`
}

func loadConfig(file string) (*ParaConfig, error) {
	var config ParaConfig
	if _, err := toml.DecodeFile(file, &config); err != nil {
		return nil, fmt.Errorf("decode parallel database config %q: %w", file, err)
	}
	if err := config.ParaDatabase.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c *ParaDatabaseConfig) validate() error {
	if c.Engine == "" {
		c.Engine = rawdb.DBPebble
	}
	if c.Engine != rawdb.DBPebble {
		return fmt.Errorf("unsupported parallel database engine %q", c.Engine)
	}
	if c.NumShards <= 0 {
		return errors.New("para-database.num-shards must be greater than zero")
	}
	if c.Path == "" {
		return errors.New("para-database.path must not be empty")
	}
	if c.CacheMB < 0 {
		return errors.New("para-database.cache-mb must not be negative")
	}
	if c.Handles <= 0 {
		c.Handles = defaultHandles
	}
	return nil
}
