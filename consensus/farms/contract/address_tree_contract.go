package contract

import (
	"bytes"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/farms/utils"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/crypto/sha3"
	"math"
	"math/big"
	"strings"
)

const (
	AddressTreeContractSlotParentOf = "0x0000000000000000000000000000000000000000000000000000000000000004"
	AddressTreeContractSlotDepthOf  = "0x0000000000000000000000000000000000000000000000000000000000000005"
)

type AddressTreeContract struct {
	state *state.StateDB

	ContractAddress common.Address
	contractABI     abi.ABI
}

func NewAddressTreeContract(state *state.StateDB, address common.Address) *AddressTreeContract {
	atABI, err := abi.JSON(strings.NewReader(systemcontracts.AddressTreeABI))
	if err != nil {
		panic(err)
	}
	return &AddressTreeContract{
		state:           state,
		contractABI:     atABI,
		ContractAddress: address,
	}
}

func (a *AddressTreeContract) ParentOf(account common.Address) common.Address {
	var slotHash common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotParentOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()

	parent := a.state.GetState(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	return common.BytesToAddress(parent.Bytes())
}

func (a *AddressTreeContract) DepthOf(account common.Address) *big.Int {
	var slotHash common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotDepthOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()

	depth := a.state.GetState(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	return new(big.Int).SetBytes(depth.Bytes())
}

func (a *AddressTreeContract) ChildrenOf(parent common.Address) *[]common.Address {
	var childrenRawDataSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(parent.Bytes(), 32))
	harsher.Write(common.LeftPadBytes([]byte("__RAW_CHILDREN"), 32))
	harsher.Sum(childrenRawDataSlot[:0])
	harsher.Reset()

	childrenRaw := a.state.GetRawState(a.ContractAddress, childrenRawDataSlot)
	childrenCount := len(childrenRaw) / common.AddressLength
	ret := make([]common.Address, childrenCount)
	for i := 0; i < childrenCount; i++ {
		ret[i] = common.BytesToAddress(childrenRaw[i*common.AddressLength : i*common.AddressLength+common.AddressLength])
	}
	return &ret
}

func (a *AddressTreeContract) AppendChild(parent common.Address, child common.Address) error {
	var childrenRawDataSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(parent.Bytes(), 32))
	harsher.Write(common.LeftPadBytes([]byte("__RAW_CHILDREN"), 32))
	harsher.Sum(childrenRawDataSlot[:0])
	harsher.Reset()

	childrenRaw := a.state.GetRawState(a.ContractAddress, childrenRawDataSlot)
	childrenRaw = append(childrenRaw, child.Bytes()...)
	a.state.SetRawState(a.ContractAddress, childrenRawDataSlot, childrenRaw)
	return nil
}

func (a *AddressTreeContract) MakeRelation(chainContext core.ChainContext, header *types.Header, chainConfig *params.ChainConfig, parent common.Address, child common.Address) error {
	method := "makeRelation"
	data, err := a.contractABI.Pack(method, parent, child)
	if err != nil {
		return err
	}
	toAddress := common.HexToAddress(systemcontracts.AddressTreeContract)
	if _, err := utils.ApplyMessage(utils.InteractiveCallMsg{
		CallMsg: ethereum.CallMsg{
			From:     header.Coinbase,
			Gas:      math.MaxUint64 / 2,
			GasPrice: big.NewInt(0),
			Value:    common.Big0,
			To:       &toAddress,
			Data:     data,
		},
	},
		a.state,
		header,
		chainConfig,
		chainContext,
	); err != nil {
		return err
	}

	return nil
}

func (a *AddressTreeContract) IsImportTransaction(tx *types.Transaction) bool {
	methodID := a.contractABI.Methods["importRelation"].ID
	return tx.To() != nil && *tx.To() == a.ContractAddress && bytes.Equal(tx.Data()[0:4], methodID)
}

func (a *AddressTreeContract) IsAddressAddedLog(log *types.Log) bool {
	eventID := a.contractABI.Events["AddressAdded"].ID
	return log.Topics[0] == eventID
}
