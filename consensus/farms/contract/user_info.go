package contract

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"golang.org/x/crypto/sha3"
	"math/big"
)

const (
	FarmMemberSlotUserInfo = 5
)

type UserInfo struct {
	state       *state.StateDB
	farmAddress common.Address

	communityPowerSlot     common.Hash
	childrenHoldAmountSlot common.Hash

	holderRewardInfoMappingSlot    common.Hash
	communityRewardInfoMappingSlot common.Hash
}

type RewardInfo struct {
	Reward     *big.Int
	RewardDebt *big.Int
}

func NewUserInfo(state *state.StateDB, farmAddress, poolAddress, account common.Address) *UserInfo {
	var slot1 common.Hash
	var slot2 common.Hash
	var childrenHoldAmountSlot common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(poolAddress.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(common.IntToSlot(FarmMemberSlotUserInfo).Bytes(), 32))
	harsher.Sum(slot1[:0])
	harsher.Reset()

	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(slot1.Bytes(), 32))
	harsher.Sum(slot2[:0])
	harsher.Reset()

	harsher.Write(common.LeftPadBytes([]byte("__ChildrenHoldAmount"), 32))
	harsher.Write(common.LeftPadBytes(poolAddress.Bytes(), 32))
	harsher.Write(common.LeftPadBytes(account.Bytes(), 32))
	harsher.Sum(childrenHoldAmountSlot[:0])
	harsher.Reset()

	slotBig := new(big.Int).SetBytes(slot2.Bytes())
	communityPowerSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(0)))
	holderRewardInfoMappingSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(1)))
	communityRewardInfoMappingSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(2)))

	return &UserInfo{
		state:       state,
		farmAddress: farmAddress,

		communityPowerSlot:             communityPowerSlot,
		childrenHoldAmountSlot:         childrenHoldAmountSlot,
		holderRewardInfoMappingSlot:    holderRewardInfoMappingSlot,
		communityRewardInfoMappingSlot: communityRewardInfoMappingSlot,
	}
}

func (u *UserInfo) GetCommunityPower() *big.Int {
	return new(big.Int).SetBytes(u.state.GetState(u.farmAddress, u.communityPowerSlot).Bytes())
}

func (u *UserInfo) SetCommunityPower(power *big.Int) {
	u.state.SetState(u.farmAddress, u.communityPowerSlot, common.BigToHash(power))
}

func (u *UserInfo) GetChildrenHoldAmount() []*big.Int {
	rawData := u.state.GetRawState(u.farmAddress, u.childrenHoldAmountSlot)
	rawDataLen := len(rawData) / 16
	ret := make([]*big.Int, rawDataLen)
	for i := 0; i < rawDataLen; i++ {
		ret[i] = new(big.Int).SetBytes(rawData[i*16 : i*16+16])
	}
	return ret
}

func (u *UserInfo) SetChildrenHoldAmount(values []*big.Int) {
	rawData := make([]byte, len(values)*16)
	for i, value := range values {
		copy(rawData[i*16:i*16+16], common.LeftPadBytes(value.Bytes(), 16))
	}
	u.state.SetRawState(u.farmAddress, u.childrenHoldAmountSlot, rawData)
}

func (u *UserInfo) GetHolderRewardInfo(rewardToken common.Address) *RewardInfo {
	var slot1 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(rewardToken.Bytes(), 32))
	harsher.Write(u.holderRewardInfoMappingSlot.Bytes())
	harsher.Sum(slot1[:0])
	harsher.Reset()

	slotBig := new(big.Int).SetBytes(slot1.Bytes())
	rewardSlot := common.BigToHash(slotBig)
	rewardDebtSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(1)))

	reward := new(big.Int).SetBytes(u.state.GetState(u.farmAddress, rewardSlot).Bytes())
	rewardDebt := new(big.Int).SetBytes(u.state.GetState(u.farmAddress, rewardDebtSlot).Bytes())

	return &RewardInfo{
		Reward:     reward,
		RewardDebt: rewardDebt,
	}
}

func (u *UserInfo) SetHolderRewardInfo(rewardToken common.Address, reward *big.Int, rewardDebt *big.Int) {
	var slot1 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(rewardToken.Bytes(), 32))
	harsher.Write(u.holderRewardInfoMappingSlot.Bytes())
	harsher.Sum(slot1[:0])
	harsher.Reset()

	slotBig := new(big.Int).SetBytes(slot1.Bytes())
	rewardSlot := common.BigToHash(slotBig)
	rewardDebtSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(1)))

	u.state.SetState(u.farmAddress, rewardSlot, common.BigToHash(reward))
	u.state.SetState(u.farmAddress, rewardDebtSlot, common.BigToHash(rewardDebt))
}

func (u *UserInfo) GetCommunityRewardInfo(rewardToken common.Address) *RewardInfo {
	var slot1 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(rewardToken.Bytes(), 32))
	harsher.Write(u.communityRewardInfoMappingSlot.Bytes())
	harsher.Sum(slot1[:0])
	harsher.Reset()

	slotBig := new(big.Int).SetBytes(slot1.Bytes())
	rewardSlot := common.BigToHash(slotBig)
	rewardDebtSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(1)))

	reward := new(big.Int).SetBytes(u.state.GetState(u.farmAddress, rewardSlot).Bytes())
	rewardDebt := new(big.Int).SetBytes(u.state.GetState(u.farmAddress, rewardDebtSlot).Bytes())

	return &RewardInfo{
		Reward:     reward,
		RewardDebt: rewardDebt,
	}
}

func (u *UserInfo) SetCommunityRewardInfo(rewardToken common.Address, reward *big.Int, rewardDebt *big.Int) {
	var slot1 common.Hash

	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes(rewardToken.Bytes(), 32))
	harsher.Write(u.communityRewardInfoMappingSlot.Bytes())
	harsher.Sum(slot1[:0])
	harsher.Reset()

	slotBig := new(big.Int).SetBytes(slot1.Bytes())
	rewardSlot := common.BigToHash(slotBig)
	rewardDebtSlot := common.BigToHash(new(big.Int).Add(slotBig, big.NewInt(1)))

	u.state.SetState(u.farmAddress, rewardSlot, common.BigToHash(reward))
	u.state.SetState(u.farmAddress, rewardDebtSlot, common.BigToHash(rewardDebt))
}
