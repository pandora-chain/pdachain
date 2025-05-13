package anchor

import (
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
)

type ChainContext struct {
	chain  consensus.ChainHeaderReader
	engine consensus.Engine
}

func NewChainContext(chain consensus.ChainHeaderReader, engine consensus.Engine) ChainContext {
	return ChainContext{
		chain:  chain,
		engine: engine,
	}
}

func (c ChainContext) Engine() consensus.Engine {
	return c.engine
}

func (c ChainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	return c.chain.GetHeader(hash, number)
}

type InteractiveCallMsg struct {
	ethereum.CallMsg
}

func (m InteractiveCallMsg) From() common.Address { return m.CallMsg.From }
func (m InteractiveCallMsg) Nonce() uint64        { return 0 }
func (m InteractiveCallMsg) CheckNonce() bool     { return false }
func (m InteractiveCallMsg) To() *common.Address  { return m.CallMsg.To }
func (m InteractiveCallMsg) GasPrice() *big.Int   { return m.CallMsg.GasPrice }
func (m InteractiveCallMsg) Gas() uint64          { return m.CallMsg.Gas }
func (m InteractiveCallMsg) Value() *big.Int      { return m.CallMsg.Value }
func (m InteractiveCallMsg) Data() []byte         { return m.CallMsg.Data }

func ApplyMessage(
	msg InteractiveCallMsg,
	state *state.StateDB,
	header *types.Header,
	chainConfig *params.ChainConfig,
	chainContext core.ChainContext,
) (uint64, error) {
	// Create a new context to be used in the EVM environment
	context := core.NewEVMBlockContext(header, chainContext, nil)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	vmenv := vm.NewEVM(context, vm.TxContext{Origin: msg.From(), GasPrice: big.NewInt(0)}, state, chainConfig, vm.Config{})
	// Apply the transaction to the current state (included in the env)
	ret, returnGas, err := vmenv.Call(
		vm.AccountRef(msg.From()),
		*msg.To(),
		msg.Data(),
		msg.Gas(),
		msg.Value(),
	)

	if err != nil {
		log.Warn("Apply message failed", "result", string(ret), "err", err)
	}
	return msg.Gas() - returnGas, err
}
