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
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ArcologyAPIRouterInterface provides the EVM hooks used by the Arcology runtime.
type ArcologyAPIRouterInterface interface {
	Call(caller, callee [20]byte, input []byte, origin [20]byte, nonce uint64, blockhash common.Hash, isStatic bool) (bool, []byte, bool, int64)
	Job() any
	PrepayGas(*uint64, *uint64) (uint64, bool)
	RefundPrepaidGas(*uint64) bool
	SetExecutionErr(error)
}

// ArcologyNetwork connects an EVM instance to the Arcology runtime.
type ArcologyNetwork struct {
	evm  *EVM
	APIs ArcologyAPIRouterInterface

	// CallContext is retained for compatibility with the original Arcology hook.
	// callContexts is the current execution ancestry, ordered root to leaf.
	CallContext  *ScopeContext
	callContexts []*ScopeContext
}

func NewArcologyNetwork(evm *EVM) *ArcologyNetwork {
	return &ArcologyNetwork{evm: evm}
}

// Call redirects a CALL or STATICCALL to Arcology when the configured router
// claims the destination. A nil router leaves normal EVM execution untouched.
func (a *ArcologyNetwork) Call(caller, addr common.Address, input []byte, gas uint64, isStatic bool) (called bool, ret []byte, leftOverGas uint64, err error) {
	if a == nil || a.APIs == nil {
		return false, nil, gas, nil
	}
	blockHash := common.Hash{}
	if a.evm.Context.GetHash != nil && a.evm.Context.BlockNumber != nil && a.evm.Context.BlockNumber.Sign() > 0 {
		previousBlock := new(big.Int).Sub(a.evm.Context.BlockNumber, big1)
		blockHash = a.evm.Context.GetHash(previousBlock.Uint64())
	}
	invoked, ret, succeeded, gasUsed := a.APIs.Call(
		caller,
		addr,
		input,
		a.evm.Origin,
		a.evm.StateDB.GetNonce(a.evm.Origin),
		blockHash,
		isStatic,
	)
	if !invoked {
		return false, ret, gas, nil
	}
	if gasUsed < 0 {
		refund := uint64(-(gasUsed + 1)) + 1
		if refund > ^uint64(0)-gas {
			leftOverGas = ^uint64(0)
		} else {
			leftOverGas = gas + refund
		}
	} else {
		used := uint64(gasUsed)
		if used > gas {
			return true, ret, 0, ErrOutOfGas
		}
		leftOverGas = gas - used
	}
	if !succeeded {
		return true, ret, leftOverGas, ErrExecutionReverted
	}
	return true, ret, leftOverGas, nil
}

func (a *ArcologyNetwork) GetCallData() []byte {
	if a == nil || a.CallContext == nil || a.CallContext.Contract == nil {
		return nil
	}
	return a.CallContext.Contract.Input
}

// CopyContext updates the currently visible interpreter context. New code
// should use enterCallContext so nested contexts are restored on return.
func (a *ArcologyNetwork) CopyContext(context interface{}) {
	callContext, ok := context.(*ScopeContext)
	if !ok {
		panic("arcology: invalid EVM call context")
	}
	a.CallContext = callContext
}

// enterCallContext records one active interpreter frame and returns a matching
// cleanup function. EVM instances are not thread-safe, so no lock is required.
func (a *ArcologyNetwork) enterCallContext(callContext *ScopeContext) func() {
	a.callContexts = append(a.callContexts, callContext)
	a.CallContext = callContext

	return func() {
		a.callContexts = a.callContexts[:len(a.callContexts)-1]
		if len(a.callContexts) == 0 {
			a.CallContext = nil
			return
		}
		a.CallContext = a.callContexts[len(a.callContexts)-1]
	}
}

func (a *ArcologyNetwork) Depth() int {
	if a == nil || a.evm == nil {
		return 0
	}
	return a.evm.depth
}

// CallHierarchy returns selector/address pairs from the current frame to the
// root frame. Arcology concatenates this ancestry to identify transition sets
// produced by nested EVM execution.
func (a *ArcologyNetwork) CallHierarchy() [][]byte {
	if a == nil {
		return nil
	}
	contexts := a.callContexts
	if len(contexts) == 0 && a.CallContext != nil {
		contexts = []*ScopeContext{a.CallContext}
	}
	buffers := make([][]byte, 0, len(contexts)*2)
	for i := len(contexts) - 1; i >= 0; i-- {
		context := contexts[i]
		if context == nil || context.Contract == nil {
			continue
		}
		selector := make([]byte, 4)
		copy(selector, context.Contract.Input)
		address := context.Contract.Address()
		buffers = append(buffers, selector, common.CopyBytes(address[:]))
	}
	return buffers
}

func (a *ArcologyNetwork) IsInConstructor() bool {
	return a != nil && a.CallContext != nil && a.CallContext.Contract != nil && a.CallContext.Contract.IsDeployment
}

func (a *ArcologyNetwork) Job() any {
	if a == nil || a.APIs == nil {
		return nil
	}
	return a.APIs.Job()
}

func (a *ArcologyNetwork) PrepayGas(initialGas, gasRemaining *uint64) (uint64, bool) {
	if a == nil || a.APIs == nil {
		return 0, true
	}
	return a.APIs.PrepayGas(initialGas, gasRemaining)
}

func (a *ArcologyNetwork) RefundPrepaidGas(gas *uint64) bool {
	if a == nil || a.APIs == nil {
		return false
	}
	return a.APIs.RefundPrepaidGas(gas)
}

func (a *ArcologyNetwork) SetExecutionErr(err error) {
	if a != nil && a.APIs != nil {
		a.APIs.SetExecutionErr(err)
	}
}
