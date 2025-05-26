package contract

import (
	"bytes"
	"context"
	"errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/farms/utils"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/systemcontracts/anchor"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/crypto/sha3"
	"math"
	"math/big"
	"strings"
)

const (
	nullAddress = "0x0000000000000000000000000000000000000000000000000000000000000000"

	/*
		0x68	headerPrefix		 	(Header)
		0x74	blockBodyPrefix	 		(Block Body)
		0x72	blockReceiptsPrefix	 	(Receipts)
		0x61	accountPrefix	 		(Account)
		0x73	storagePrefix	 		(Storage Slot)
		0x63	codePrefix	 			(Contract Code)
		0x74	txLookupPrefix	 		(Transaction Lookup)
		0x6e	snapshotAccountPrefix	(Snapshot)
	*/
)

var (
	errFetchStateFromRemoteState = errors.New("fetch address tree node from remote state failed")
	errWriteStateToRawDB         = errors.New("write state to cache rawdb failed")
	errReadStateFromRawDB        = errors.New("read state from cache rawdb failed")
	errBatchCommitToRawDB        = errors.New("commit batch to rawdb failed")
)

type AddressTreeContract struct {
	state   *state.StateDB
	cacheDB *ethdb.Database

	ContractAddress common.Address
	contractABI     abi.ABI

	anchorClient *ethclient.Client
	treeVersion  uint64
}

func NewAddressTreeContract(state *state.StateDB, cacheDb *ethdb.Database, address common.Address, anchorClient *ethclient.Client, useVersion uint64) *AddressTreeContract {
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
		cacheDB:         cacheDb,
	}
}

func (a *AddressTreeContract) inAnchorNet() bool {
	return a.cacheDB != nil && a.anchorClient != nil && a.treeVersion > 0
}

func (a *AddressTreeContract) storageAt(addr common.Address, hash common.Hash) (common.Hash, error) {
	if a.inAnchorNet() {
		ret, err := a.anchorClient.StorageAt(context.TODO(), addr, hash, nil)
		if err != nil {
			return common.Hash{}, err
		}
		return common.BytesToHash(ret), nil
	} else {
		return a.state.GetState(addr, hash), nil
	}
}

func (a *AddressTreeContract) rawStorageAt(addr common.Address, hash common.Hash) ([]byte, error) {
	if a.inAnchorNet() {
		ret, err := a.anchorClient.RawStorageAt(context.TODO(), addr, hash, nil)
		if err != nil {
			return []byte{}, err
		}
		return ret, nil
	} else {
		return a.state.GetRawState(addr, hash), nil
	}
}

func (a *AddressTreeContract) cacheParentOf(account common.Address) common.Address {
	parentBytes, _ := (*a.cacheDB).Get(anchor.ParentDBKey(account))
	if parentBytes == nil {
		return common.Address{}
	}
	return common.BytesToAddress(parentBytes)
}

func (a *AddressTreeContract) cacheVersionOf(account common.Address) *big.Int {
	versionBytes, _ := (*a.cacheDB).Get(anchor.VersionDBKey(account))
	if versionBytes == nil {
		return big.NewInt(0)
	}
	return big.NewInt(0).SetBytes(versionBytes)
}

func (a *AddressTreeContract) cacheDepthOf(account common.Address) *big.Int {
	depthBytes, _ := (*a.cacheDB).Get(anchor.DepthDBKey(account))
	if depthBytes == nil {
		return big.NewInt(0)
	}
	return big.NewInt(0).SetBytes(depthBytes)
}

func (a *AddressTreeContract) tryCacheAccountNode(account common.Address) error {

	// is already cached
	if a.cacheParentOf(account) != common.HexToAddress(nullAddress) {
		return nil
	}

	versionBytes, err := a.storageAt(a.ContractAddress, anchor.VersionSlotHash(account))
	if err != nil {
		return errFetchStateFromRemoteState
	}

	parentBytes, err := a.storageAt(a.ContractAddress, anchor.ParentSlotHash(account))
	if err != nil {
		return errFetchStateFromRemoteState
	}

	depthBytes, err := a.storageAt(a.ContractAddress, anchor.DepthSlotHash(account))
	if err != nil {
		return errFetchStateFromRemoteState
	}

	parent := common.HashToAddress(parentBytes)
	version := big.NewInt(0).SetBytes(versionBytes.Bytes())
	depth := big.NewInt(0).SetBytes(depthBytes.Bytes())

	if version.Uint64() == 0 && depth.Uint64() > 0 && parent != common.HexToAddress(nullAddress) {
		v := big.NewInt(0).SetUint64(a.treeVersion - 1)
		putBatch := (*a.cacheDB).NewBatch()

		if putBatch.Put(anchor.VersionDBKey(account), common.BigToHash(v).Bytes()) != nil {
			return errWriteStateToRawDB
		}

		if putBatch.Put(anchor.ParentDBKey(account), parentBytes.Bytes()) != nil {
			return errWriteStateToRawDB
		}

		if putBatch.Put(anchor.DepthDBKey(account), depthBytes.Bytes()) != nil {
			return errWriteStateToRawDB
		}

		if putBatch.Write() != nil {
			return errBatchCommitToRawDB
		}

	} else if version.Uint64() < a.treeVersion && depth.Uint64() > 0 && parent != common.HexToAddress(nullAddress) {
		putBatch := (*a.cacheDB).NewBatch()

		if putBatch.Put(anchor.VersionDBKey(account), versionBytes.Bytes()) != nil {
			return errWriteStateToRawDB
		}

		if putBatch.Put(anchor.ParentDBKey(account), parentBytes.Bytes()) != nil {
			return errWriteStateToRawDB
		}

		if putBatch.Put(anchor.DepthDBKey(account), depthBytes.Bytes()) != nil {
			return errWriteStateToRawDB
		}

		if putBatch.Write() != nil {
			return errBatchCommitToRawDB
		}
	}

	return nil
}

func (a *AddressTreeContract) ParentOf(account common.Address) (common.Address, error) {
	if a.inAnchorNet() {
		if err := a.tryCacheAccountNode(account); err != nil {
			return common.Address{}, err
		}
		return a.cacheParentOf(account), nil
	} else {
		parent, err := a.storageAt(common.HexToAddress(systemcontracts.AddressTreeContract), anchor.ParentSlotHash(account))
		if err != nil {
			return common.Address{}, err
		}
		return common.BytesToAddress(parent.Bytes()), nil
	}
}

func (a *AddressTreeContract) DepthOf(account common.Address) (*big.Int, error) {
	if a.inAnchorNet() {
		if err := a.tryCacheAccountNode(account); err != nil {
			return nil, err
		}
		return a.cacheDepthOf(account), nil
	} else {
		depth, err := a.storageAt(a.ContractAddress, anchor.DepthSlotHash(account))
		if err != nil {
			return nil, err
		}
		return new(big.Int).SetBytes(depth.Bytes()), nil
	}
}

func (a *AddressTreeContract) ChildrenOf(parent common.Address) (*[]common.Address, error) {
	childrenRaw, err := a.rawStorageAt(a.ContractAddress, anchor.ChildrenSlotHash(parent))
	if err != nil {
		return nil, err
	}

	childrenCount := len(childrenRaw) / common.AddressLength
	ret := make([]common.Address, 0)
	for i := 0; i < childrenCount; i++ {
		child := common.BytesToAddress(childrenRaw[i*common.AddressLength : i*common.AddressLength+common.AddressLength])
		if a.inAnchorNet() {
			if err := a.tryCacheAccountNode(child); err != nil {
				return nil, err
			}
			if a.cacheParentOf(child) != parent {
				break
			}
		}
		ret = append(ret, child)
	}

	if a.inAnchorNet() {
		var rawChildren []byte
		for _, address := range ret {
			rawChildren = append(rawChildren, address.Bytes()...)
		}
		if (*a.cacheDB).Put(anchor.ChildrenDBKey(parent), rawChildren) != nil {
			return nil, errWriteStateToRawDB
		}
	}

	return &ret, nil
}

func (a *AddressTreeContract) AppendChild(parent common.Address, child common.Address) error {
	// Illegal Method Invocation in Anchor Consensus
	if a.inAnchorNet() {
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
	if a.inAnchorNet() {
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
	if a.inAnchorNet() {
		panic("Anchor network cannot write data")
	}
	methodID := a.contractABI.Methods["importRelation"].ID
	return tx.To() != nil && *tx.To() == a.ContractAddress && bytes.Equal(tx.Data()[0:4], methodID)
}

func (a *AddressTreeContract) IsAddressAddedLog(log *types.Log) bool {
	if a.inAnchorNet() {
		panic("Anchor network cannot write data")
	}
	eventID := a.contractABI.Events["AddressAdded"].ID
	return log.Topics[0] == eventID
}
