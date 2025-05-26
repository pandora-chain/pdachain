// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"bytes"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/systemcontracts/anchor"
	"github.com/ethereum/go-ethereum/ethdb"
	"golang.org/x/crypto/sha3"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

const (
	MainNetChainID = 140705
)

// emptyCodeHash is used by create to ensure deployment is disallowed to already
// deployed contract addresses (relevant after the account abstraction).
var emptyCodeHash = crypto.Keccak256Hash(nil)

var EvmPool = sync.Pool{
	New: func() interface{} {
		return &EVM{}
	},
}

type (
	// CanTransferFunc is the signature of a transfer guard function
	CanTransferFunc func(StateDB, common.Address, *big.Int) bool
	// TransferFunc is the signature of a transfer function
	TransferFunc func(StateDB, common.Address, common.Address, *big.Int)
	// GetHashFunc returns the n'th block hash in the blockchain
	// and is used by the BLOCKHASH EVM op code.
	GetHashFunc func(uint64) common.Hash
)

func (evm *EVM) precompile(addr common.Address) (PrecompiledContract, bool) {
	var precompiles map[common.Address]PrecompiledContract
	switch {
	case evm.chainRules.IsMoran:
		precompiles = PrecompiledContractsIsMoran
	case evm.chainRules.IsNano:
		precompiles = PrecompiledContractsIsNano
	case evm.chainRules.IsBerlin:
		precompiles = PrecompiledContractsBerlin
	case evm.chainRules.IsIstanbul:
		precompiles = PrecompiledContractsIstanbul
	case evm.chainRules.IsByzantium:
		precompiles = PrecompiledContractsByzantium
	default:
		precompiles = PrecompiledContractsHomestead
	}
	p, ok := precompiles[addr]
	return p, ok
}

// BlockContext provides the EVM with auxiliary information. Once provided
// it shouldn't be modified.
type BlockContext struct {
	// CanTransfer returns whether the account contains
	// sufficient ether to transfer the value
	CanTransfer CanTransferFunc
	// Transfer transfers ether from one account to the other
	Transfer TransferFunc
	// GetHash returns the hash corresponding to n
	GetHash GetHashFunc

	// Block information
	Coinbase    common.Address // Provides information for COINBASE
	GasLimit    uint64         // Provides information for GASLIMIT
	BlockNumber *big.Int       // Provides information for NUMBER
	Time        *big.Int       // Provides information for TIME
	Difficulty  *big.Int       // Provides information for DIFFICULTY
	BaseFee     *big.Int       // Provides information for BASEFEE
	Random      *common.Hash   // Provides information for RANDOM
}

// TxContext provides the EVM with information about a transaction.
// All fields can change between transactions.
type TxContext struct {
	// Message information
	Origin   common.Address // Provides information for ORIGIN
	GasPrice *big.Int       // Provides information for GASPRICE
}

// EVM is the Ethereum Virtual Machine base object and provides
// the necessary tools to run a contract on the given state with
// the provided context. It should be noted that any error
// generated through any of the calls should be considered a
// revert-state-and-consume-all-gas operation, no checks on
// specific errors should ever be performed. The interpreter makes
// sure that any errors generated are to be considered faulty code.
//
// The EVM should never be reused and is not thread safe.
type EVM struct {
	// Context provides auxiliary blockchain related information
	Context BlockContext
	TxContext
	// StateDB gives access to the underlying state
	StateDB StateDB
	// Depth is the current call stack
	depth int

	// chainConfig contains information about the current chain
	chainConfig *params.ChainConfig
	// chain rules contains the chain rules for the current epoch
	chainRules params.Rules
	// virtual machine configuration options used to initialise the
	// evm.
	Config Config
	// global (to this context) ethereum virtual machine
	// used throughout the execution of the tx.
	interpreter *EVMInterpreter
	// abort is used to abort the EVM calling operations
	// NOTE: must be set atomically
	abort int32
	// callGasTemp holds the gas available for the current call. This is needed because the
	// available gas is calculated in gasCall* according to the 63/64 rule and later
	// applied in opCall*.
	callGasTemp uint64

	treeABI abi.ABI
	cacheDB *ethdb.Database
}

// NewEVM returns a new EVM. The returned EVM is not thread safe and should
// only ever be used *once*.
func NewEVM(blockCtx BlockContext, txCtx TxContext, statedb StateDB, chainConfig *params.ChainConfig, config Config) *EVM {
	evm := EvmPool.Get().(*EVM)
	evm.Context = blockCtx
	evm.TxContext = txCtx
	evm.StateDB = statedb
	evm.Config = config
	evm.chainConfig = chainConfig
	evm.chainRules = chainConfig.Rules(blockCtx.BlockNumber, blockCtx.Random != nil)
	evm.abort = 0
	evm.callGasTemp = 0
	evm.depth = 0
	evm.interpreter = NewEVMInterpreter(evm, config)

	if chainConfig.Anchor != nil && chainConfig.Anchor.CacheDataBase != nil {
		abi, err := abi.JSON(strings.NewReader(`[{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"versionOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"childrenOf","outputs":[{"internalType":"address[]","name":"","type":"address[]"}],"stateMutability":"view","type":"function"}]`))
		if err != nil {
			panic(err)
		}

		evm.treeABI = abi
		evm.cacheDB = &chainConfig.Anchor.CacheDataBase
	}

	return evm
}

func (evm *EVM) IsAnchorEVM() bool {
	return evm.chainConfig.Anchor != nil && evm.chainConfig.Anchor.CacheDataBase != nil && evm.cacheDB != nil
}

// Reset resets the EVM with a new transaction context.Reset
// This is not threadsafe and should only be done very cautiously.
func (evm *EVM) Reset(txCtx TxContext, statedb StateDB) {
	evm.TxContext = txCtx
	evm.StateDB = statedb
}

// Cancel cancels any running EVM operation. This may be called concurrently and
// it's safe to be called multiple times.
func (evm *EVM) Cancel() {
	atomic.StoreInt32(&evm.abort, 1)
}

// Cancelled returns true if Cancel has been called
func (evm *EVM) Cancelled() bool {
	return atomic.LoadInt32(&evm.abort) == 1
}

// Interpreter returns the current interpreter
func (evm *EVM) Interpreter() *EVMInterpreter {
	return evm.interpreter
}

// Call executes the contract associated with the addr with the given input as
// parameters. It also handles any necessary value transfer required and takes
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
func (evm *EVM) Call(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error) {
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// Fail if we're trying to transfer more than the available balance
	if value.Sign() != 0 && !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}

	hooked := false
	ret, leftOverGas, err = evm.callHook(caller, addr, input, gas, &hooked)
	if hooked {
		return ret, leftOverGas, err
	}

	snapshot := evm.StateDB.Snapshot()
	p, isPrecompile := evm.precompile(addr)

	if !evm.StateDB.Exist(addr) {
		if !isPrecompile && evm.chainRules.IsEIP158 && value.Sign() == 0 {
			// Calling a non existing account, don't do anything, but ping the tracer
			if evm.Config.Debug {
				if evm.depth == 0 {
					evm.Config.Tracer.CaptureStart(evm, caller.Address(), addr, false, input, gas, value)
					evm.Config.Tracer.CaptureEnd(ret, 0, 0, nil)
				} else {
					evm.Config.Tracer.CaptureEnter(CALL, caller.Address(), addr, input, gas, value)
					evm.Config.Tracer.CaptureExit(ret, 0, nil)
				}
			}
			return nil, gas, nil
		}
		evm.StateDB.CreateAccount(addr)
	}
	evm.Context.Transfer(evm.StateDB, caller.Address(), addr, value)

	// Capture the tracer start/end events in debug mode
	if evm.Config.Debug {
		if evm.depth == 0 {
			evm.Config.Tracer.CaptureStart(evm, caller.Address(), addr, false, input, gas, value)
			defer func(startGas uint64, startTime time.Time) { // Lazy evaluation of the parameters
				evm.Config.Tracer.CaptureEnd(ret, startGas-gas, time.Since(startTime), err)
			}(gas, time.Now())
		} else {
			// Handle tracer events for entering and exiting a call frame
			evm.Config.Tracer.CaptureEnter(CALL, caller.Address(), addr, input, gas, value)
			defer func(startGas uint64) {
				evm.Config.Tracer.CaptureExit(ret, startGas-gas, err)
			}(gas)
		}
	}

	if isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		code := evm.StateDB.GetCode(addr)
		if len(code) == 0 {
			ret, err = nil, nil // gas is unchanged
		} else {
			addrCopy := addr
			// If the account has no code, we can abort here
			// The depth-check is already done, and precompiles handled above
			contract := NewContract(caller, AccountRef(addrCopy), value, gas)
			contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), code)
			ret, err = evm.interpreter.Run(contract, input, false)
			gas = contract.Gas
		}
	}
	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, gas, err
}

// CallCode executes the contract associated with the addr with the given input
// as parameters. It also handles any necessary value transfer required and takes
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
//
// CallCode differs from Call in the sense that it executes the given address'
// code with the caller as context.
func (evm *EVM) CallCode(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error) {
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// Fail if we're trying to transfer more than the available balance
	// Note although it's noop to transfer X ether to caller itself. But
	// if caller doesn't have enough balance, it would be an error to allow
	// over-charging itself. So the check here is necessary.
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}
	var snapshot = evm.StateDB.Snapshot()

	// Invoke tracer hooks that signal entering/exiting a call frame
	if evm.Config.Debug {
		evm.Config.Tracer.CaptureEnter(CALLCODE, caller.Address(), addr, input, gas, value)
		defer func(startGas uint64) {
			evm.Config.Tracer.CaptureExit(ret, startGas-gas, err)
		}(gas)
	}

	// It is allowed to call precompiles, even via delegatecall
	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		addrCopy := addr
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		contract := NewContract(caller, AccountRef(caller.Address()), value, gas)
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), evm.StateDB.GetCode(addrCopy))
		ret, err = evm.interpreter.Run(contract, input, false)
		gas = contract.Gas
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// DelegateCall executes the contract associated with the addr with the given input
// as parameters. It reverses the state in case of an execution error.
//
// DelegateCall differs from CallCode in the sense that it executes the given address'
// code with the caller as context and the caller is set to the caller of the caller.
func (evm *EVM) DelegateCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	var snapshot = evm.StateDB.Snapshot()

	// Invoke tracer hooks that signal entering/exiting a call frame
	if evm.Config.Debug {
		evm.Config.Tracer.CaptureEnter(DELEGATECALL, caller.Address(), addr, input, gas, nil)
		defer func(startGas uint64) {
			evm.Config.Tracer.CaptureExit(ret, startGas-gas, err)
		}(gas)
	}

	// It is allowed to call precompiles, even via delegatecall
	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		addrCopy := addr
		// Initialise a new contract and make initialise the delegate values
		contract := NewContract(caller, AccountRef(caller.Address()), nil, gas).AsDelegate()
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), evm.StateDB.GetCode(addrCopy))
		ret, err = evm.interpreter.Run(contract, input, false)
		gas = contract.Gas
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// StaticCall executes the contract associated with the addr with the given input
// as parameters while disallowing any modifications to the state during the call.
// Opcodes that attempt to perform such modifications will result in exceptions
// instead of performing the modifications.
func (evm *EVM) StaticCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	hooked := false
	ret, leftOverGas, err = evm.callHook(caller, addr, input, gas, &hooked)
	if hooked {
		return ret, leftOverGas, err
	}

	// We take a snapshot here. This is a bit counter-intuitive, and could probably be skipped.
	// However, even a staticcall is considered a 'touch'. On mainnet, static calls were introduced
	// after all empty accounts were deleted, so this is not required. However, if we omit this,
	// then certain tests start failing; stRevertTest/RevertPrecompiledTouchExactOOG.json.
	// We could change this, but for now it's left for legacy reasons
	var snapshot = evm.StateDB.Snapshot()

	// We do an AddBalance of zero here, just in order to trigger a touch.
	// This doesn't matter on Mainnet, where all empties are gone at the time of Byzantium,
	// but is the correct thing to do and matters on other networks, in tests, and potential
	// future scenarios
	evm.StateDB.AddBalance(addr, big0)

	// Invoke tracer hooks that signal entering/exiting a call frame
	if evm.Config.Debug {
		evm.Config.Tracer.CaptureEnter(STATICCALL, caller.Address(), addr, input, gas, nil)
		defer func(startGas uint64) {
			evm.Config.Tracer.CaptureExit(ret, startGas-gas, err)
		}(gas)
	}

	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		// At this point, we use a copy of address. If we don't, the go compiler will
		// leak the 'contract' to the outer scope, and make allocation for 'contract'
		// even if the actual execution ends on RunPrecompiled above.
		addrCopy := addr
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		contract := NewContract(caller, AccountRef(addrCopy), new(big.Int), gas)
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), evm.StateDB.GetCode(addrCopy))
		// When an error was returned by the EVM or when setting the creation code
		// above we revert to the snapshot and consume any gas remaining. Additionally
		// when we're in Homestead this also counts for code storage gas errors.
		ret, err = evm.interpreter.Run(contract, input, true)
		gas = contract.Gas
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

type codeAndHash struct {
	code []byte
	hash common.Hash
}

func (c *codeAndHash) Hash() common.Hash {
	if c.hash == (common.Hash{}) {
		c.hash = crypto.Keccak256Hash(c.code)
	}
	return c.hash
}

// create creates a new contract using code as deployment code.
func (evm *EVM) create(caller ContractRef, codeAndHash *codeAndHash, gas uint64, value *big.Int, address common.Address, typ OpCode) ([]byte, common.Address, uint64, error) {
	// Depth check execution. Fail if we're trying to execute above the
	// limit.
	if evm.isPrivateDeploymentMode() && !evm.isContractCreator(caller.Address()) {
		activeBlockNumber := uint64(0)
		if evm.chainConfig.ChainID.Uint64() == MainNetChainID {
			activeBlockNumber = 1547266
		}

		if evm.Context.BlockNumber.Uint64() > activeBlockNumber {
			nonce := evm.StateDB.GetNonce(caller.Address())
			if nonce+1 < nonce {
				return nil, common.Address{}, gas, ErrNonceUintOverflow
			}
			evm.StateDB.SetNonce(caller.Address(), nonce+1)
		}
		return nil, common.Address{}, gas, ErrNoDeploymentPermission
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, common.Address{}, gas, ErrDepth
	}
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, common.Address{}, gas, ErrInsufficientBalance
	}
	nonce := evm.StateDB.GetNonce(caller.Address())
	if nonce+1 < nonce {
		return nil, common.Address{}, gas, ErrNonceUintOverflow
	}
	evm.StateDB.SetNonce(caller.Address(), nonce+1)
	// We add this to the access list _before_ taking a snapshot. Even if the creation fails,
	// the access-list change should not be rolled back
	if evm.chainRules.IsBerlin {
		evm.StateDB.AddAddressToAccessList(address)
	}
	// Ensure there's no existing contract already at the designated address
	contractHash := evm.StateDB.GetCodeHash(address)
	if evm.StateDB.GetNonce(address) != 0 || (contractHash != (common.Hash{}) && contractHash != emptyCodeHash) {
		return nil, common.Address{}, 0, ErrContractAddressCollision
	}
	// Create a new account on the state
	snapshot := evm.StateDB.Snapshot()
	evm.StateDB.CreateAccount(address)
	if evm.chainRules.IsEIP158 {
		evm.StateDB.SetNonce(address, 1)
	}
	evm.Context.Transfer(evm.StateDB, caller.Address(), address, value)

	// Initialise a new contract and set the code that is to be used by the EVM.
	// The contract is a scoped environment for this execution context only.
	contract := NewContract(caller, AccountRef(address), value, gas)
	contract.SetCodeOptionalHash(&address, codeAndHash)

	if evm.Config.Debug {
		if evm.depth == 0 {
			evm.Config.Tracer.CaptureStart(evm, caller.Address(), address, true, codeAndHash.code, gas, value)
		} else {
			evm.Config.Tracer.CaptureEnter(typ, caller.Address(), address, codeAndHash.code, gas, value)
		}
	}

	start := time.Now()

	ret, err := evm.interpreter.Run(contract, nil, false)

	// Check whether the max code size has been exceeded, assign err if the case.
	if err == nil && evm.chainRules.IsEIP158 && len(ret) > params.MaxCodeSize {
		err = ErrMaxCodeSizeExceeded
	}

	// Reject code starting with 0xEF if EIP-3541 is enabled.
	if err == nil && len(ret) >= 1 && ret[0] == 0xEF && evm.chainRules.IsLondon {
		err = ErrInvalidCode
	}

	// if the contract creation ran successfully and no errors were returned
	// calculate the gas required to store the code. If the code could not
	// be stored due to not enough gas set an error and let it be handled
	// by the error checking condition below.
	if err == nil {
		createDataGas := uint64(len(ret)) * params.CreateDataGas
		if contract.UseGas(createDataGas) {
			evm.StateDB.SetCode(address, ret)
		} else {
			err = ErrCodeStoreOutOfGas
		}
	}

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil && (evm.chainRules.IsHomestead || err != ErrCodeStoreOutOfGas) {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}

	if evm.Config.Debug {
		if evm.depth == 0 {
			evm.Config.Tracer.CaptureEnd(ret, gas-contract.Gas, time.Since(start), err)
		} else {
			evm.Config.Tracer.CaptureExit(ret, gas-contract.Gas, err)
		}
	}
	return ret, address, contract.Gas, err
}

// Create creates a new contract using code as deployment code.
func (evm *EVM) Create(caller ContractRef, code []byte, gas uint64, value *big.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error) {
	contractAddr = crypto.CreateAddress(caller.Address(), evm.StateDB.GetNonce(caller.Address()))
	return evm.create(caller, &codeAndHash{code: code}, gas, value, contractAddr, CREATE)
}

// Create2 creates a new contract using code as deployment code.
//
// The different between Create2 with Create is Create2 uses keccak256(0xff ++ msg.sender ++ salt ++ keccak256(init_code))[12:]
// instead of the usual sender-and-nonce-hash as the address where the contract is initialized at.
func (evm *EVM) Create2(caller ContractRef, code []byte, gas uint64, endowment *big.Int, salt *uint256.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error) {
	codeAndHash := &codeAndHash{code: code}
	contractAddr = crypto.CreateAddress2(caller.Address(), salt.Bytes32(), codeAndHash.Hash().Bytes())
	return evm.create(caller, codeAndHash, gas, endowment, contractAddr, CREATE2)
}

// ChainConfig returns the environment's chain configuration
func (evm *EVM) ChainConfig() *params.ChainConfig { return evm.chainConfig }

func (evm *EVM) callHook(caller ContractRef, addr common.Address, input []byte, gas uint64, hooked *bool) (ret []byte, leftOverGas uint64, err error) {
	// Special handling
	// `function holderRangeInfoOf(address token,uint64 rangeIndex)` in contract 0xC '0e603a1c'
	// `function holderRangeAccRewardPerShare(address,uint64)` in contract 0xC '24fc55d9'
	// `function childrenOf(address owner)` in contract 0xA '42c4c0d0'
	// `function childrenHoldAmount(address,address)` in contract 0xC 'e8b23ad8'

	if addr == common.HexToAddress(systemcontracts.FarmContract) && len(input) >= 4 {
		if strings.EqualFold(common.Bytes2Hex(input[0:4]), "0e603a1c") && len(input) == 68 {
			tokenContract := common.BytesToAddress(input[4:36])
			rangeIndex := new(big.Int).SetBytes(input[36:68])
			*hooked = true
			return evm.holderRangeInfo(tokenContract, rangeIndex, gas)
		}

		if strings.EqualFold(common.Bytes2Hex(input[0:4]), "24fc55d9") && len(input) == 100 {
			poolAddress := common.BytesToAddress(input[4:36])
			rewardTokenAddress := common.BytesToAddress(input[36:68])
			rangeIndex := new(big.Int).SetBytes(input[68:100])
			*hooked = true
			return evm.holderRangeAccRewardPerShare(poolAddress, rewardTokenAddress, rangeIndex, gas)
		}

		if strings.EqualFold(common.Bytes2Hex(input[0:4]), "e8b23ad8") && len(input) == 68 {
			poolAddress := common.BytesToAddress(input[4:36])
			parentAddress := common.BytesToAddress(input[36:68])
			*hooked = true
			return evm.childrenHoldAmount(poolAddress, parentAddress, gas)
		}

	} else if evm.chainConfig.Anchor == nil && addr == common.HexToAddress(systemcontracts.AddressTreeContract) && len(input) >= 4 {
		if strings.EqualFold(common.Bytes2Hex(input[0:4]), "42c4c0d0") && len(input) == 36 {
			parentAddress := common.BytesToAddress(input[4:36])
			*hooked = true
			return evm.childrenOf(parentAddress, gas)
		}
	} else if evm.chainConfig.Anchor != nil && evm.IsAnchorEVM() && addr == common.HexToAddress(systemcontracts.AddressTreeContract) && len(input) == 36 {
		var result hexutil.Bytes
		account := common.BytesToAddress(input[4:36])
		*hooked = true
		// Method ID
		// 		depthOf:    7c3165b1
		//  	parentOf:   ee08388e
		//  	versionOf:  0db3ff45
		//  	childrenOf: 42c4c0d0
		switch common.Bytes2Hex(input[0:4]) {

		case "7c3165b1":
			// depthOf
			result = evm.cacheStateDepthOf(account)
			break

		case "ee08388e":
			// parentOf
			result = evm.cacheStateParentOf(account)
			break

		case "0db3ff45":
			// versionOf
			result = evm.cacheStateVersionOf(account)
			break

		case "42c4c0d0":
			// childrenOf
			result = evm.cacheStateChildrenOf(account)
			break

		default:
			*hooked = false
		}

		if *hooked {
			return result, 0, nil
		} else {
			return []byte{}, 0, nil
		}
	}
	*hooked = false
	return []byte{}, 0, nil
}

func (evm *EVM) holderRangeAccRewardPerShare(pool common.Address, rewardToken common.Address, rangeIndex *big.Int, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if gas < 20000 {
		return nil, gas, ErrOutOfGas
	}

	var rewardPerShareSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write([]byte("__RewardPerShare"))
	harsher.Write(pool.Bytes())
	harsher.Write(rewardToken.Bytes())
	harsher.Sum(rewardPerShareSlot[:0])
	harsher.Reset()

	rewardPerShareRaw := evm.StateDB.GetRawState(common.HexToAddress(systemcontracts.FarmContract), rewardPerShareSlot)
	if len(rewardPerShareRaw) == 0 {
		return make([]byte, 32), gas - 20000, nil
	} else {
		totalRangeCount := len(rewardPerShareRaw) / 24
		rIndex := rangeIndex.Uint64()
		if rIndex >= uint64(totalRangeCount) {
			rIndex = uint64(totalRangeCount) - 1
		}
		ret := common.LeftPadBytes(rewardPerShareRaw[rIndex*24+0:rIndex*24+24], 32)
		return ret, gas - 20000, nil
	}
}

func (evm *EVM) holderRangeInfo(tokenContract common.Address, rangeIndex *big.Int, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if gas < 40000 {
		return nil, gas, ErrOutOfGas
	}

	var rawDataSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes([]byte("__HolderDistribution"), 32))
	harsher.Write(common.LeftPadBytes(tokenContract.Bytes(), 32))
	harsher.Sum(rawDataSlot[:0])
	harsher.Reset()

	rawData := evm.StateDB.GetRawState(common.HexToAddress(systemcontracts.FarmContract), rawDataSlot)

	if len(rawData) <= 0 {
		return make([]byte, 64), gas - 40000, nil
	} else {
		totalRangeCount := len(rawData) / 7
		rIndex := rangeIndex.Uint64()
		if rIndex >= uint64(totalRangeCount) {
			rIndex = uint64(totalRangeCount) - 1
		}

		totalCount := rawData[rIndex*7+0 : rIndex*7+4]
		emptyRangeCount := rawData[rIndex*7+4 : rIndex*7+4+3]

		ret := append(common.LeftPadBytes(totalCount, 32), common.LeftPadBytes(emptyRangeCount, 32)...)
		return ret, gas - 80000, nil
	}
}

func (evm *EVM) childrenHoldAmount(poolAddress common.Address, parent common.Address, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if gas < 40000 {
		return nil, gas, ErrOutOfGas
	}

	var childrenHoldAmountSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes([]byte("__ChildrenHoldAmount"), 32))
	harsher.Write(common.LeftPadBytes(poolAddress.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(parent.Bytes(), 32))
	harsher.Sum(childrenHoldAmountSlot[:0])
	harsher.Reset()

	rawData := evm.StateDB.GetRawState(common.HexToAddress(systemcontracts.FarmContract), childrenHoldAmountSlot)
	rawDataLen := len(rawData) / 16

	ret1 := [][]byte{
		common.LeftPadBytes(big.NewInt(32).Bytes(), 32),
		common.LeftPadBytes(big.NewInt(int64(rawDataLen)).Bytes(), 32),
	}
	for i := 0; i < rawDataLen; i++ {
		ret1 = append(ret1, common.LeftPadBytes(rawData[i*16:i*16+16], 32))
	}

	return bytes.Join(ret1, []byte{}), gas - 40000, nil
}

func (evm *EVM) childrenOf(parent common.Address, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if gas < 40000 {
		return nil, gas, ErrOutOfGas
	}

	var childrenRawDataSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(parent.Bytes(), 32))
	harsher.Write(common.LeftPadBytes([]byte("__RAW_CHILDREN"), 32))
	harsher.Sum(childrenRawDataSlot[:0])
	harsher.Reset()

	childrenRaw := evm.StateDB.GetRawState(common.HexToAddress(systemcontracts.AddressTreeContract), childrenRawDataSlot)
	childrenLen := len(childrenRaw) / common.AddressLength

	ret1 := [][]byte{
		common.LeftPadBytes(big.NewInt(32).Bytes(), 32),
		common.LeftPadBytes(big.NewInt(int64(childrenLen)).Bytes(), 32),
	}
	for i := 0; i < childrenLen; i++ {
		ret1 = append(ret1, common.LeftPadBytes(childrenRaw[i*common.AddressLength:i*common.AddressLength+common.AddressLength], 32))
	}

	return bytes.Join(ret1, []byte{}), gas - 40000, nil
}

func (evm *EVM) isContractCreator(caller common.Address) bool {
	var slot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(caller.Bytes(), 32))

	if evm.IsAnchorEVM() {
		harsher.Write(common.LeftPadBytes(common.IntToSlot(5).Bytes(), 32))
	} else {
		harsher.Write(common.LeftPadBytes(common.IntToSlot(7).Bytes(), 32))
	}

	harsher.Sum(slot[:0])
	harsher.Reset()

	boolBytes := evm.StateDB.GetState(common.HexToAddress(systemcontracts.SystemDaoContract), slot)
	return common.StateToBig(boolBytes).Uint64() > 0
}

func (evm *EVM) isPrivateDeploymentMode() bool {
	boolBytes := evm.StateDB.GetState(common.HexToAddress(systemcontracts.SystemDaoContract), common.BigToHash(big.NewInt(6)))
	return common.StateToBig(boolBytes).Uint64() > 0
}

// //////////////////////////////////////////////////////////////////////////////////////
// anchor network addressTree support methods
// //////////////////////////////////////////////////////////////////////////////////////
const (
	AddressTreeContractSlotParentOf  = "0x0000000000000000000000000000000000000000000000000000000000000004"
	AddressTreeContractSlotDepthOf   = "0x0000000000000000000000000000000000000000000000000000000000000005"
	AddressTreeContractSlotVersionOf = "0x0000000000000000000000000000000000000000000000000000000000000006"
)

func (evm *EVM) cacheStateChildrenOf(account common.Address) []byte {
	childrenRaw, err := (*evm.cacheDB).Get(anchor.ChildrenDBKey(account))
	if err != nil {
		emptyEncode := [][]byte{
			common.LeftPadBytes(big.NewInt(32).Bytes(), 32),
			common.LeftPadBytes(big.NewInt(0).Bytes(), 32),
		}
		return bytes.Join(emptyEncode, []byte{})
	}
	childrenLen := len(childrenRaw) / common.AddressLength
	ret1 := [][]byte{
		common.LeftPadBytes(big.NewInt(32).Bytes(), 32),
		common.LeftPadBytes(big.NewInt(int64(childrenLen)).Bytes(), 32),
	}
	for i := 0; i < childrenLen; i++ {
		ret1 = append(ret1, common.LeftPadBytes(childrenRaw[i*common.AddressLength:i*common.AddressLength+common.AddressLength], 32))
	}

	return bytes.Join(ret1, []byte{})
}

func (evm *EVM) cacheStateParentOf(account common.Address) []byte {
	parentRaw, _ := (*evm.cacheDB).Get(anchor.ParentDBKey(account))
	if parentRaw == nil {
		return common.LeftPadBytes([]byte{}, 32)
	}
	return parentRaw
}

func (evm *EVM) cacheStateVersionOf(account common.Address) []byte {
	versionRaw, _ := (*evm.cacheDB).Get(anchor.VersionDBKey(account))
	if versionRaw == nil {
		return common.BigToHash(big.NewInt(0)).Bytes()
	}
	return versionRaw
}

func (evm *EVM) cacheStateDepthOf(account common.Address) []byte {
	depthRaw, _ := (*evm.cacheDB).Get(anchor.DepthDBKey(account))
	if depthRaw == nil {
		return common.BigToHash(big.NewInt(0)).Bytes()
	}
	return depthRaw
}
