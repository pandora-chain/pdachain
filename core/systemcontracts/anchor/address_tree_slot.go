package anchor

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/sha3"
)

const (
	AddressTreeContractSlotParentOf  = "0x0000000000000000000000000000000000000000000000000000000000000004"
	AddressTreeContractSlotDepthOf   = "0x0000000000000000000000000000000000000000000000000000000000000005"
	AddressTreeContractSlotVersionOf = "0x0000000000000000000000000000000000000000000000000000000000000006"
)

func ldbKey(slotHash common.Hash) []byte {
	prefix := []byte{0x73} // storagePrefix
	accountHash := crypto.Keccak256(common.HexToHash(systemcontracts.AddressTreeContract).Bytes())
	key := append(prefix, accountHash...)
	key = append(key, slotHash.Bytes()...)
	return key
}

func ChildrenSlotHash(account common.Address) common.Hash {
	var slotHash common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes([]byte("__RAW_CHILDREN"), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()
	return slotHash
}

func ParentSlotHash(account common.Address) common.Hash {
	var slotHash common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotParentOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()
	return slotHash
}

func VersionSlotHash(account common.Address) common.Hash {
	var slotHash common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotVersionOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()
	return slotHash
}

func DepthSlotHash(account common.Address) common.Hash {
	var slotHash common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.FromHex(AddressTreeContractSlotDepthOf), 32))
	harsher.Sum(slotHash[:0])
	harsher.Reset()
	return slotHash
}

func ChildrenDBKey(account common.Address) []byte {
	return ldbKey(ChildrenSlotHash(account))
}

func ParentDBKey(account common.Address) []byte {
	return ldbKey(ParentSlotHash(account))
}

func VersionDBKey(account common.Address) []byte {
	return ldbKey(VersionSlotHash(account))
}

func DepthDBKey(account common.Address) []byte {
	return ldbKey(DepthSlotHash(account))
}
