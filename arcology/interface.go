package arcology

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// KernelAPI provides system level function calls supported by Monaco platform.
type KernelAPI interface {
	AddLog(key, value string)
	Call(caller, callee common.Address, input []byte, origin common.Address, nonce uint64, blockhash common.Hash) (bool, []byte, bool)
	Prepare(common.Hash, *big.Int, uint32)
	SetCallContext(context interface{})
}
