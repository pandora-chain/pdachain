package contract

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/farms/utils"
	"github.com/ethereum/go-ethereum/core/state"
	"golang.org/x/crypto/sha3"
	"math/big"
)

const (
	FarmMemberSlotPools                      = 2
	FarmMemberSlotCommunityAccRewardPerShare = 4
)

type FarmContract struct {
	address     common.Address
	state       *state.StateDB
	isAnchorNet bool
}

func NewFarmContract(state *state.StateDB, address common.Address, isAnchorNet bool) *FarmContract {
	return &FarmContract{
		state:       state,
		address:     address,
		isAnchorNet: isAnchorNet,
	}
}

func (fc *FarmContract) ContractAddress() common.Address {
	return fc.address
}

func (fc *FarmContract) GetPools() []common.Address {
	values := utils.GetHashArrayState(fc.state, fc.address, common.IntToSlot(FarmMemberSlotPools))
	ret := make([]common.Address, len(*values))
	for i := 0; i < len(*values); i++ {
		ret[i] = common.HashToAddress((*values)[i])
	}
	return ret
}

func (fc *FarmContract) GetPoolInfo(pool common.Address) *PoolInfo {
	return NewPoolInfo(fc.state, fc.address, pool)
}

func (fc *FarmContract) GetUserInfo(pool common.Address, account common.Address) *UserInfo {
	return NewUserInfo(fc.state, fc.address, pool, account)
}

func (fc *FarmContract) GetCommunityAccRewardPerShare(pool common.Address, rewardToken common.Address) *big.Int {
	var slot1 common.Hash
	var slot2 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(pool.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.IntToSlot(FarmMemberSlotCommunityAccRewardPerShare).Bytes(), 32))
	harsher.Sum(slot1[:0])
	harsher.Reset()

	harsher.Write(common.LeftPadBytes(rewardToken.Bytes(), 32))
	harsher.Write(slot1.Bytes())
	harsher.Sum(slot2[:0])

	return new(big.Int).SetBytes(fc.state.GetState(fc.address, slot2).Bytes())
}

func (fc *FarmContract) SetCommunityAccRewardPerShare(pool common.Address, rewardToken common.Address, accRewardPerShare *big.Int) {
	var slot1 common.Hash
	var slot2 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(pool.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.IntToSlot(FarmMemberSlotCommunityAccRewardPerShare).Bytes(), 32))
	harsher.Sum(slot1[:0])
	harsher.Reset()

	harsher.Write(common.LeftPadBytes(rewardToken.Bytes(), 32))
	harsher.Write(slot1.Bytes())
	harsher.Sum(slot2[:0])

	fc.state.SetState(fc.address, slot2, common.BigToHash(accRewardPerShare))
}

func (fc *FarmContract) GetParentLastUpdateBlock(pool common.Address, account common.Address) *big.Int {
	if !fc.isAnchorNet {
		panic("GetLastUpdateBlock method only work AnchorNet")
	}

	var slot1 common.Hash
	var slot2 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(pool.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.IntToSlot(FarmMemberSlotLastUpdateBlockOf).Bytes(), 32))
	harsher.Sum(slot1[:0])
	harsher.Reset()

	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(slot1.Bytes(), 32))
	harsher.Sum(slot2[:0])
	harsher.Reset()

	lastUpdateBlock := fc.state.GetState(fc.address, slot2)
	return new(big.Int).SetBytes(lastUpdateBlock.Bytes())
}

func (fc *FarmContract) SetParentLastUpdateBlock(pool common.Address, account common.Address, number *big.Int) {
	if !fc.isAnchorNet {
		panic("GetLastUpdateBlock method only work AnchorNet")
	}

	var slot1 common.Hash
	var slot2 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(pool.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.IntToSlot(FarmMemberSlotLastUpdateBlockOf).Bytes(), 32))
	harsher.Sum(slot1[:0])
	harsher.Reset()

	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(slot1.Bytes(), 32))
	harsher.Sum(slot2[:0])
	harsher.Reset()

	fc.state.SetState(fc.address, slot2, common.BigToHash(number))
}
