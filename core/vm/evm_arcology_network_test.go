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

package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

type arcologyTestStateDB struct {
	StateDB
	nonce uint64
}

func (s *arcologyTestStateDB) GetNonce(common.Address) uint64 { return s.nonce }

type arcologyTestRouter struct {
	caller   common.Address
	callee   common.Address
	origin   common.Address
	nonce    uint64
	block    common.Hash
	isStatic bool
	success  bool
	gasUsed  int64
}

func (r *arcologyTestRouter) Call(caller, callee [20]byte, _ []byte, origin [20]byte, nonce uint64, block common.Hash, isStatic bool) (bool, []byte, bool, int64) {
	r.caller = caller
	r.callee = callee
	r.origin = origin
	r.nonce = nonce
	r.block = block
	r.isStatic = isStatic
	return true, []byte{0xaa}, r.success, r.gasUsed
}

func (*arcologyTestRouter) Job() any                                  { return nil }
func (*arcologyTestRouter) PrepayGas(*uint64, *uint64) (uint64, bool) { return 0, true }
func (*arcologyTestRouter) RefundPrepaidGas(*uint64) bool             { return false }
func (*arcologyTestRouter) SetExecutionErr(error)                     {}

func TestArcologyCallWithoutRouterFallsThrough(t *testing.T) {
	network := NewArcologyNetwork(&EVM{})
	called, ret, gas, err := network.Call(common.Address{}, common.Address{}, nil, 42, false)
	if called || ret != nil || gas != 42 || err != nil {
		t.Fatalf("unexpected fallthrough result: called=%v ret=%x gas=%d err=%v", called, ret, gas, err)
	}
}

func TestArcologyCallInterception(t *testing.T) {
	caller := common.HexToAddress("0x01")
	callee := common.HexToAddress("0x02")
	origin := common.HexToAddress("0x03")
	previousBlockHash := common.HexToHash("0x04")
	router := &arcologyTestRouter{success: true, gasUsed: 7}
	evm := &EVM{
		Context: BlockContext{
			BlockNumber: big.NewInt(9),
			GetHash: func(number uint64) common.Hash {
				if number != 8 {
					t.Fatalf("previous block number mismatch: have %d want 8", number)
				}
				return previousBlockHash
			},
		},
		TxContext: TxContext{Origin: origin},
		StateDB:   &arcologyTestStateDB{nonce: 11},
	}
	evm.ArcologyAPIs = NewArcologyNetwork(evm)
	evm.ArcologyAPIs.APIs = router

	ret, gas, err := evm.Call(caller, callee, []byte{0x01}, 100, uint256.NewInt(0))
	if err != nil || !bytes.Equal(ret, []byte{0xaa}) || gas != 93 {
		t.Fatalf("unexpected interception result: ret=%x gas=%d err=%v", ret, gas, err)
	}
	if router.caller != caller || router.callee != callee || router.origin != origin || router.nonce != 11 || router.block != previousBlockHash || router.isStatic {
		t.Fatalf("unexpected router arguments: %+v", router)
	}

	_, _, err = evm.StaticCall(caller, callee, nil, 100)
	if err != nil || !router.isStatic {
		t.Fatalf("STATICCALL was not marked static: static=%v err=%v", router.isStatic, err)
	}
}

func TestArcologyCallRejectsGasUnderflow(t *testing.T) {
	router := &arcologyTestRouter{success: true, gasUsed: 101}
	evm := &EVM{
		Context:   BlockContext{BlockNumber: big.NewInt(0)},
		TxContext: TxContext{},
		StateDB:   &arcologyTestStateDB{},
	}
	evm.ArcologyAPIs = NewArcologyNetwork(evm)
	evm.ArcologyAPIs.APIs = router

	_, gas, err := evm.Call(common.Address{}, common.Address{}, nil, 100, uint256.NewInt(0))
	if err != ErrOutOfGas || gas != 0 {
		t.Fatalf("unexpected over-budget result: gas=%d err=%v", gas, err)
	}
}

func TestArcologyCallChargesFailedExecution(t *testing.T) {
	router := &arcologyTestRouter{success: false, gasUsed: 7}
	evm := &EVM{
		Context:   BlockContext{BlockNumber: big.NewInt(0)},
		TxContext: TxContext{},
		StateDB:   &arcologyTestStateDB{},
	}
	evm.ArcologyAPIs = NewArcologyNetwork(evm)
	evm.ArcologyAPIs.APIs = router

	_, gas, err := evm.Call(common.Address{}, common.Address{}, nil, 100, uint256.NewInt(0))
	if err != ErrExecutionReverted || gas != 93 {
		t.Fatalf("unexpected failed-execution result: gas=%d err=%v", gas, err)
	}
}

func TestArcologyCallHierarchy(t *testing.T) {
	network := NewArcologyNetwork(&EVM{})
	rootAddress := common.HexToAddress("0x01")
	childAddress := common.HexToAddress("0x02")

	root := NewContract(common.Address{}, rootAddress, uint256.NewInt(0), 0, newMapJumpDests())
	root.Input = []byte{0x01, 0x02, 0x03, 0x04, 0xff}
	leaveRoot := network.enterCallContext(&ScopeContext{Contract: root})

	child := NewContract(rootAddress, childAddress, uint256.NewInt(0), 0, newMapJumpDests())
	child.Input = []byte{0xaa}
	leaveChild := network.enterCallContext(&ScopeContext{Contract: child})

	hierarchy := network.CallHierarchy()
	want := [][]byte{
		{0xaa, 0x00, 0x00, 0x00}, childAddress[:],
		{0x01, 0x02, 0x03, 0x04}, rootAddress[:],
	}
	if len(hierarchy) != len(want) {
		t.Fatalf("hierarchy length mismatch: have %d want %d", len(hierarchy), len(want))
	}
	for i := range want {
		if !bytes.Equal(hierarchy[i], want[i]) {
			t.Fatalf("hierarchy item %d mismatch: have %x want %x", i, hierarchy[i], want[i])
		}
	}

	// Returned identifiers must not alias live call-frame data.
	hierarchy[0][0] = 0xff
	if child.Input[0] != 0xaa {
		t.Fatal("hierarchy selector aliases contract input")
	}

	leaveChild()
	hierarchy = network.CallHierarchy()
	if len(hierarchy) != 2 || !bytes.Equal(hierarchy[1], rootAddress[:]) {
		t.Fatalf("parent context was not restored: %x", hierarchy)
	}

	leaveRoot()
	if hierarchy := network.CallHierarchy(); len(hierarchy) != 0 {
		t.Fatalf("hierarchy was not cleared: %x", hierarchy)
	}
}
