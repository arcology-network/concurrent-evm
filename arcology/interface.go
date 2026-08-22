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

package arcology

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// KernelAPI provides system level function calls supported by Arcology platform.
type KernelAPI interface {
	AddLog(key, value string)
	Call(caller, callee common.Address, input []byte, origin common.Address, nonce uint64, blockhash common.Hash) (bool, []byte, bool)
	Prepare(common.Hash, *big.Int, uint32)
	SetCallContext(context interface{})
}
