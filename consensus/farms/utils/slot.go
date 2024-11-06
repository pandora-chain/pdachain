package utils

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"golang.org/x/crypto/sha3"
	"math/big"
)

func GetHashArrayState(state *state.StateDB, contract common.Address, startSlot common.Hash) *[]common.Hash {
	poolsLen := common.StateToBig(state.GetState(contract, startSlot)).Int64()
	ret := make([]common.Hash, poolsLen)

	var arraySlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(startSlot.Bytes())
	harsher.Sum(arraySlot[:0])
	harsher.Reset()

	slotBig := new(big.Int).SetBytes(arraySlot.Bytes())

	for i := int64(0); i < poolsLen; i++ {
		ret[i] = state.GetState(contract, common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(i))))
	}

	return &ret
}
