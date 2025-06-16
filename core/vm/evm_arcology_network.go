package vm

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type ArcologyNetwork struct {
	evm         *EVM
	CallContext *ScopeContext              // only available at run time
	APIs        ArcologyAPIRouterInterface // Arcology API entrance
}

func NewArcologyNetwork(evm *EVM) *ArcologyNetwork {	
	api := &ArcologyNetwork{
		evm: evm,		
	}
	return api
}

// Redirect to Arcology API intead
func (this ArcologyNetwork) Call(callerContract ContractRef, addr common.Address, input []byte, gas uint64, isReadOnly bool) (called bool, ret []byte, leftOverGas uint64, err error) {
	// Not constructor because the APIs are set after the EVM is itialized.
	if this.CallContext != nil && this.CallContext != nil && this.CallContext.Contract.API == nil {
		this.CallContext.Contract.API = this.APIs
	}

	if successfullyCalled, ret, ok, gasUsed := this.APIs.Call(
		callerContract.Address(),
		addr,
		input,
		this.evm.Origin,
		this.evm.StateDB.GetNonce(this.evm.Origin),
		this.evm.Context.GetHash(new(big.Int).Sub(this.evm.Context.BlockNumber, big1).Uint64()),
		isReadOnly,
	); successfullyCalled {
		if gasUsed < 0 { // Refund or reallocate gas
			leftOverGas = gas + uint64(gasUsed*-1)
		} else {
			leftOverGas = gas - uint64(gasUsed)
		}

		if !ok {
			return true, ret, leftOverGas, ErrExecutionReverted
		}
		return true, ret, leftOverGas, nil
	}
	return false, ret, gas, nil
}

func (this *ArcologyNetwork) GetCallData() []byte {
	if this.CallContext.Contract != nil {
		return (this.CallContext.Contract.Input)
	}
	return []byte{}
}

func (this *ArcologyNetwork) CopyContext(context interface{}) {
	this.CallContext = context.(*ScopeContext)
}

func (this *ArcologyNetwork) Depth() int { return this.evm.depth }

func (this *ArcologyNetwork) CallHierarchy() [][]byte {
	addr := this.CallContext.Contract.Address()

	buffers := [][]byte{
		this.CallContext.Contract.Input[:4],
		addr[:],
	}

	if IsType[*Contract](this.CallContext.Contract.caller) { // Not a contract
		caller := this.CallContext.Contract.caller
		callerAddr := caller.Address()
		for {
			if !IsType[*Contract](caller) { // Not a contract
				break
			}
			buffers = append(append(buffers, caller.(*Contract).Input[:4]), callerAddr[:])

			caller = caller.(*Contract).caller
		}
	}
	return buffers
}

func (this *ArcologyNetwork) IsInConstructor() bool {
	return this.CallContext.Contract.CodeHash == common.Hash{}
}
// func (this *ArcologyNetwork) GetExecutionSubsidy() uint64 { return this.APIs.GetExecutionSubsidy() }
