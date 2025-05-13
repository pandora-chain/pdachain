package contract

import (
	"bytes"
	"context"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/farms/utils"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/crypto/sha3"
	"math"
	"math/big"
	"strings"
)

const (
	AddressTreeContractSlotParentOf  = "0x0000000000000000000000000000000000000000000000000000000000000004"
	AddressTreeContractSlotDepthOf   = "0x0000000000000000000000000000000000000000000000000000000000000005"
	AddressTreeContractSlotVersionOf = "0x0000000000000000000000000000000000000000000000000000000000000006"
)

type AddressTreeContract struct {
	state *state.StateDB

	ContractAddress common.Address
	contractABI     abi.ABI

	anchorClient *ethclient.Client
	treeVersion  uint64
}

func NewAddressTreeContract(state *state.StateDB, address common.Address, anchorClient *ethclient.Client, useVersion uint64) *AddressTreeContract {
	atABI, err := abi.JSON(strings.NewReader(systemcontracts.AddressTreeABI))
	if err != nil {
		panic(err)
	}
	return &AddressTreeContract{
		state:           state,
		contractABI:     atABI,
		ContractAddress: address,
		anchorClient:    anchorClient,
		treeVersion:     useVersion,
	}
}

func (a *AddressTreeContract) isAnchorNet() bool {
	return a.anchorClient != nil
}

func (a *AddressTreeContract) storageAt(addr common.Address, hash common.Hash) (common.Hash, error) {
	if a.anchorClient == nil && a.state != nil {
		return a.state.GetState(addr, hash), nil
	} else if a.anchorClient != nil {
		ret, err := a.anchorClient.StorageAt(context.TODO(), addr, hash, nil)
		if err != nil {
			return common.Hash{}, err
		}
		return common.BytesToHash(ret), nil
	} else {
		panic(`state and anchor client invalid`)
	}
}

func (a *AddressTreeContract) rawStorageAt(addr common.Address, hash common.Hash) ([]byte, error) {
	if a.anchorClient == nil && a.state != nil {
		return a.state.GetRawState(addr, hash), nil
	} else if a.anchorClient != nil {
		ret, err := a.anchorClient.RawStorageAt(context.TODO(), addr, hash, nil)
		if err != nil {
			return []byte{}, err
		}
		return ret, nil
	} else {
		panic(`state and anchor client invalid`)
	}
}

func (a *AddressTreeContract) VersionOf(account common.Address) (*big.Int, error) {
	var slotHash common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotVersionOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()

	//depth := a.state.GetState(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	version, err := a.storageAt(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(version.Bytes()), nil
}

func (a *AddressTreeContract) ParentOf(account common.Address) (common.Address, error) {

	// check tree node version
	if a.treeVersion > 0 {
		ver, err := a.VersionOf(account)
		if err != nil {
			return common.Address{}, err
		}

		if ver.Uint64() > a.treeVersion {
			return common.Address{}, nil
		}
	}

	var slotHash common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotParentOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()

	//parent := a.state.GetState(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	parent, err := a.storageAt(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	if err != nil {
		return common.Address{}, err
	}

	return common.BytesToAddress(parent.Bytes()), nil
}

func (a *AddressTreeContract) DepthOf(account common.Address) (*big.Int, error) {

	// check tree node version
	if a.treeVersion > 0 {
		ver, err := a.VersionOf(account)
		if err != nil {
			return big.NewInt(0), err
		}
		if ver.Uint64() > a.treeVersion {
			return big.NewInt(0), nil
		}
	}

	var slotHash common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotDepthOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()

	//depth := a.state.GetState(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	depth, err := a.storageAt(common.HexToAddress(systemcontracts.AddressTreeContract), slotHash)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(depth.Bytes()), nil
}

func (a *AddressTreeContract) ChildrenOf(parent common.Address) (*[]common.Address, error) {
	var childrenRawDataSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(parent.Bytes(), 32))
	harsher.Write(common.LeftPadBytes([]byte("__RAW_CHILDREN"), 32))
	harsher.Sum(childrenRawDataSlot[:0])
	harsher.Reset()

	//childrenRaw := a.state.GetRawState(a.ContractAddress, childrenRawDataSlot)
	childrenRaw, err := a.rawStorageAt(a.ContractAddress, childrenRawDataSlot)
	if err != nil {
		return nil, err
	}

	childrenCount := len(childrenRaw) / common.AddressLength
	ret := make([]common.Address, 0)
	for i := 0; i < childrenCount; i++ {
		child := common.BytesToAddress(childrenRaw[i*common.AddressLength : i*common.AddressLength+common.AddressLength])

		if a.treeVersion > 0 {
			ver, err := a.VersionOf(child)
			if err != nil {
				return nil, err
			}
			if ver.Uint64() > a.treeVersion {
				break
			}
		}

		ret = append(ret, child)
	}

	return &ret, nil
}

func (a *AddressTreeContract) AppendChild(parent common.Address, child common.Address) error {
	// Illegal Method Invocation in Anchor Consensus
	if a.isAnchorNet() {
		panic("Anchor network cannot write data")
	}

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
	// Illegal Method Invocation in Anchor Consensus
	if a.isAnchorNet() {
		panic("Anchor network cannot write data")
	}

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
