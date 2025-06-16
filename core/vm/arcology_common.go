/*
 *   Copyright (c) 2025 Arcology Network

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

 package vm

import (
	"github.com/ethereum/go-ethereum/common"
)
 
func IsType[T any](v interface{}) bool {
	switch v.(type) {
	case T:
		return true
	}
	return false
}

// KernelAPI provides system level function calls supported by arcology platform.
type ArcologyAPIRouterInterface interface {
	// SetExecutionSubsidy(uint64)  // Kept on Arcology side for clarity.
	// GetExecutionSubsidy() uint64 // Get the execution subsidy for the current call
	Call(caller, callee [20]byte, input []byte, origin [20]byte, nonce uint64, blockhash common.Hash, isStatic bool) (bool, []byte, bool, int64)
	UseSponsoredGas(gas uint64) (bool) // Use sponsored gas for the current call
}


