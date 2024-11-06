package common

import (
	"math/big"
)

func StateToBig(hash Hash) *big.Int {
	return new(big.Int).SetBytes(hash.Bytes())
}

func BigToSlot(slot *big.Int) Hash { return BigToHash(slot) }

func IntToSlot(slot int64) Hash { return BigToHash(big.NewInt(slot)) }

func HashToAddress(hash Hash) Address {
	return BytesToAddress(hash.Bytes())
}
