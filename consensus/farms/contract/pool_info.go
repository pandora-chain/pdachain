package contract

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/farms/utils"
	"github.com/ethereum/go-ethereum/core/state"
	"golang.org/x/crypto/sha3"
	"math/big"
)

const (
	FarmMemberSlotPoolOf = 3
)

type PoolInfo struct {
	state       *state.StateDB
	farmAddress common.Address

	token               common.Address
	holderRangeCount    *big.Int
	holderRangeInterval *big.Int

	holderTotalPowerSlot    common.Hash
	communityTotalPowerSlot common.Hash

	rewardTokensArraySlot       common.Hash
	rewardTokensLockerArraySlot common.Hash

	rewardStartRangeIndex *big.Int
}

func NewPoolInfo(state *state.StateDB, farmAddress, poolAddress common.Address) *PoolInfo {
	var poolInfoSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(poolAddress.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.IntToSlot(FarmMemberSlotPoolOf).Bytes(), 32))
	harsher.Sum(poolInfoSlot[:0])
	harsher.Reset()

	// Struct Slots
	slotBig := new(big.Int).SetBytes(poolInfoSlot.Bytes())
	tokenSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(0)))
	holderRangeCountSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(1)))
	holderRangeIntervalSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(2)))
	holderTotalPowerSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(3)))
	communityTotalPowerSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(4)))
	rewardTokensSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(5)))
	rewardLockersSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(6)))
	rewardStartRangeIndexSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(8)))

	token := state.GetState(farmAddress, tokenSlot)
	holderRangeCount := state.GetState(farmAddress, holderRangeCountSlot)
	holderRangeInterval := state.GetState(farmAddress, holderRangeIntervalSlot)
	rewardStartRangeIndex := state.GetState(farmAddress, rewardStartRangeIndexSlot)

	return &PoolInfo{
		state:                       state,
		farmAddress:                 farmAddress,
		token:                       common.BytesToAddress(token.Bytes()),
		holderRangeCount:            new(big.Int).SetBytes(holderRangeCount.Bytes()),
		holderRangeInterval:         new(big.Int).SetBytes(holderRangeInterval.Bytes()),
		holderTotalPowerSlot:        holderTotalPowerSlot,
		communityTotalPowerSlot:     communityTotalPowerSlot,
		rewardTokensArraySlot:       rewardTokensSlot,
		rewardTokensLockerArraySlot: rewardLockersSlot,
		rewardStartRangeIndex:       new(big.Int).SetBytes(rewardStartRangeIndex.Bytes()),
	}
}

func (p *PoolInfo) GetTokenAddress() common.Address {
	return p.token
}

func (p *PoolInfo) GetRangeCount() *big.Int {
	return p.holderRangeCount
}

func (p *PoolInfo) GetRangeInterval() *big.Int {
	return p.holderRangeInterval
}

func (p *PoolInfo) GetRewardStartRangeIndex() *big.Int { return p.rewardStartRangeIndex }

func (p *PoolInfo) GetHolderTotalPower() *big.Int {
	return new(big.Int).SetBytes(p.state.GetState(p.farmAddress, p.holderTotalPowerSlot).Bytes())
}

func (p *PoolInfo) GetCommunityTotalPower() *big.Int {
	return new(big.Int).SetBytes(p.state.GetState(p.farmAddress, p.communityTotalPowerSlot).Bytes())
}

func (p *PoolInfo) GetRewardTokens() []common.Address {
	hashArray := utils.GetHashArrayState(p.state, p.farmAddress, p.rewardTokensArraySlot)
	ret := make([]common.Address, len(*hashArray))
	for i := 0; i < len(ret); i++ {
		ret[i] = common.HashToAddress((*hashArray)[i])
	}
	return ret
}

func (p *PoolInfo) GetRewardLocks() []common.Address {
	hashArray := utils.GetHashArrayState(p.state, p.farmAddress, p.rewardTokensLockerArraySlot)
	ret := make([]common.Address, len(*hashArray))
	for i := 0; i < len(ret); i++ {
		ret[i] = common.HashToAddress((*hashArray)[i])
	}
	return ret
}

func (p *PoolInfo) SetHolderTotalPower(power *big.Int) {
	p.state.SetState(p.farmAddress, p.holderTotalPowerSlot, common.BigToHash(power))
}

func (p *PoolInfo) SetCommunityTotalPower(power *big.Int) {
	p.state.SetState(p.farmAddress, p.communityTotalPowerSlot, common.BigToHash(power))
}
