package farms

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/farms/contract"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/crypto/sha3"
	"math"
	"math/big"
	"strings"
	"time"
)

const (
	NullAddress = "0x0000000000000000000000000000000000000000"
	BurnAddress = "0x000000000000000000000000000000000000dead"

	// ActivePowerMultipleMaxLimit unit: ETHER
	ActivePowerMultipleMaxLimit = uint64(10000)
	TreeHeightMaxLimit          = 200

	BenchMarkPrint = false
)

type RangeInfo struct {
	rangeIndex      uint64
	totalCount      *big.Int // uint32
	emptyRangeCount *big.Int // uint24
}

type PoolDistribution struct {
	state               *state.StateDB
	ethAPI              *ethapi.PublicBlockChainAPI
	erc20ABI            abi.ABI
	farmContract        *contract.FarmContract
	addressTreeContract *contract.AddressTreeContract

	farmAddress common.Address
	poolAddress common.Address
	poolInfo    *contract.PoolInfo

	distributionRawData     []byte
	distributionRawDataSlot common.Hash
	rewardPerShares         *map[common.Address][]byte
	rewardPerSharesSlot     *map[common.Address]common.Hash

	isFork0815         bool
	blockBalanceCaches map[common.Address]*big.Int
}

func newTokenHolderDistribution(state *state.StateDB, ethAPI *ethapi.PublicBlockChainAPI, farmContract *contract.FarmContract, addressTreeContract *contract.AddressTreeContract, pool common.Address, poolInfo *contract.PoolInfo, isFork0815 bool) *PoolDistribution {

	ercABI, err := abi.JSON(strings.NewReader(systemcontracts.ERC20ABI))
	if err != nil {
		panic(err)
	}

	var rawDataSlot common.Hash
	harsher := sha3.NewLegacyKeccak256()
	harsher.Write(common.LeftPadBytes([]byte("__HolderDistribution"), 32))
	harsher.Write(common.LeftPadBytes(pool.Bytes(), 32))
	harsher.Sum(rawDataSlot[:0])
	harsher.Reset()

	farm := farmContract.ContractAddress()
	rawData := state.GetRawState(farm, rawDataSlot)
	if len(rawData) == 0 {
		rawData = make([]byte, poolInfo.GetRangeCount().Uint64()*7)
	}

	r := &PoolDistribution{
		farmAddress:             farm,
		farmContract:            farmContract,
		addressTreeContract:     addressTreeContract,
		poolAddress:             pool,
		state:                   state,
		ethAPI:                  ethAPI,
		erc20ABI:                ercABI,
		poolInfo:                poolInfo,
		distributionRawData:     common.CopyBytes(rawData),
		distributionRawDataSlot: rawDataSlot,
		rewardPerShares:         &map[common.Address][]byte{},
		rewardPerSharesSlot:     &map[common.Address]common.Hash{},
		isFork0815:              isFork0815,
		blockBalanceCaches:      map[common.Address]*big.Int{},
	}

	for _, address := range poolInfo.GetRewardTokens() {
		var rewardPerShareSlot common.Hash
		harsher.Write([]byte("__RewardPerShare"))
		harsher.Write(pool.Bytes())
		harsher.Write(address.Bytes())
		harsher.Sum(rewardPerShareSlot[:0])
		harsher.Reset()

		rewardPerShareRaw := state.GetRawState(farm, rewardPerShareSlot)
		if len(rewardPerShareRaw) == 0 {
			rewardPerShareRaw = make([]byte, 24*poolInfo.GetRangeCount().Uint64())
		} else {
			rewardPerShareRaw = common.CopyBytes(rewardPerShareRaw)
		}
		(*r.rewardPerShares)[address] = rewardPerShareRaw
		(*r.rewardPerSharesSlot)[address] = rewardPerShareSlot
	}

	return r
}

func (d *PoolDistribution) putTransferEventLog(blockHash common.Hash, from common.Address, to common.Address, amount *big.Int) error {

	if from == to {
		return nil
	}

	if from != common.HexToAddress(NullAddress) && from != common.HexToAddress(BurnAddress) && d.state.GetCodeSize(from) == 0 {
		fromOriginBalance, err := d.balanceOf(blockHash, from)
		if err != nil {
			return err
		}
		fromCurrentBalance := new(big.Int).Sub(fromOriginBalance, amount)
		if err := d.updateAccountBalance(from, fromOriginBalance, fromCurrentBalance); err != nil {
			return err
		}
	}

	if to != common.HexToAddress(NullAddress) && to != common.HexToAddress(BurnAddress) && d.state.GetCodeSize(to) == 0 {
		toOriginBalance, err := d.balanceOf(blockHash, to)
		if err != nil {
			return err
		}
		toCurrentBalance := new(big.Int).Add(toOriginBalance, amount)
		if err := d.updateAccountBalance(to, toOriginBalance, toCurrentBalance); err != nil {
			return err
		}
	}

	return nil
}

func (d *PoolDistribution) sort() {
	emptyRangeTotalCount := big.NewInt(0)
	totalPower := big.NewInt(0)

	for i := uint64(1); i < d.poolInfo.GetRangeCount().Uint64(); i++ {
		rangeInfo := d.GetRangeInfo(i)
		rangeInfo.emptyRangeCount = emptyRangeTotalCount
		d.SetRangeInfo(i, rangeInfo)

		if rangeInfo.totalCount.Cmp(common.Big0) > 0 {
			totalPower = new(big.Int).Add(
				totalPower, new(big.Int).Mul(
					big.NewInt(int64(i)-rangeInfo.emptyRangeCount.Int64()),
					rangeInfo.totalCount,
				),
			)
		} else {
			emptyRangeTotalCount = new(big.Int).Add(emptyRangeTotalCount, common.Big1)
		}
	}

	d.poolInfo.SetHolderTotalPower(totalPower)
}

func (d *PoolDistribution) saveDistributionRawData() {
	d.state.SetRawState(d.farmAddress, d.distributionRawDataSlot, d.distributionRawData)
}

func (d *PoolDistribution) saveRewardPerShares() {
	for _, token := range d.poolInfo.GetRewardTokens() {
		d.state.SetRawState(d.farmAddress, (*d.rewardPerSharesSlot)[token], (*d.rewardPerShares)[token])
	}
}

func (d *PoolDistribution) Storage() {
	d.sort()
	d.saveDistributionRawData()
	d.saveRewardPerShares()
}

func (d *PoolDistribution) GetRangeInfo(rangeIndex uint64) *RangeInfo {
	data := d.distributionRawData[rangeIndex*7 : rangeIndex*7+7]
	return &RangeInfo{
		rangeIndex:      rangeIndex,
		totalCount:      big.NewInt(0).SetBytes(data[0:4]),
		emptyRangeCount: big.NewInt(0).SetBytes(data[4:7]),
	}
}

func (d *PoolDistribution) SetRangeInfo(rangeIndex uint64, rangeInfo *RangeInfo) {
	copy(d.distributionRawData[rangeIndex*7:rangeIndex*7+4], common.LeftPadBytes(rangeInfo.totalCount.Bytes(), 4))
	copy(d.distributionRawData[rangeIndex*7+4:rangeIndex*7+4+3], common.LeftPadBytes(rangeInfo.emptyRangeCount.Bytes(), 3))
}

func (d *PoolDistribution) GetHolderRewardPerShare(rewardToken common.Address, rangeIndex uint64) *big.Int {
	return new(big.Int).SetBytes((*d.rewardPerShares)[rewardToken][rangeIndex*24 : rangeIndex*24+24])
}

func (d *PoolDistribution) SetHolderRewardPerShare(rewardToken common.Address, rangeIndex uint64, rewardPerShare *big.Int) {
	copy((*d.rewardPerShares)[rewardToken][rangeIndex*24:rangeIndex*24+24], common.LeftPadBytes(rewardPerShare.Bytes(), 24))
}

func (d *PoolDistribution) UpdateRewardPerShares(rewardToken common.Address, holderReward, communityReward *big.Int) {
	d.sort()

	if d.poolInfo.GetHolderTotalPower().Cmp(common.Big0) > 0 {
		powerPerShare := new(big.Int).Div(holderReward, d.poolInfo.GetHolderTotalPower())
		for i := d.poolInfo.GetRewardStartRangeIndex().Uint64(); i < d.poolInfo.GetRangeCount().Uint64(); i++ {
			originRangePerShare := d.GetHolderRewardPerShare(rewardToken, i)
			rangeInfo := d.GetRangeInfo(i)
			appendPerShare := new(big.Int).Mul(powerPerShare, big.NewInt(int64(i)-rangeInfo.emptyRangeCount.Int64()))
			d.SetHolderRewardPerShare(rewardToken, i, new(big.Int).Add(originRangePerShare, appendPerShare))
		}
	}

	if d.poolInfo.GetCommunityTotalPower().Cmp(common.Big0) > 0 {
		originCommunityPerShare := d.farmContract.GetCommunityAccRewardPerShare(d.poolAddress, rewardToken)
		appendCommunityPerShare := new(big.Int).Div(communityReward, d.poolInfo.GetCommunityTotalPower())
		d.farmContract.SetCommunityAccRewardPerShare(d.poolAddress, rewardToken, new(big.Int).Add(originCommunityPerShare, appendCommunityPerShare))
	}
}

func (d *PoolDistribution) balanceOf(blockHash common.Hash, account common.Address) (*big.Int, error) {

	if balance, isExisted := d.blockBalanceCaches[account]; isExisted && d.isFork0815 {
		return balance, nil
	}

	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)
	method := "balanceOf"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming

	data, err := d.erc20ABI.Pack(method, account)
	if err != nil {
		return big.NewInt(0), err
	}

	// call
	msgData := (hexutil.Bytes)(data)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := d.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &d.poolAddress,
		Data: &msgData,
	}, blockNr, nil)
	if err != nil {
		return nil, err
	}

	var ret0 *big.Int
	if err := d.erc20ABI.UnpackIntoInterface(&ret0, method, result); err != nil {
		return nil, err
	}
	d.blockBalanceCaches[account] = ret0

	return ret0, nil
}

func (d *PoolDistribution) updateAccountBalance(from common.Address, originAmount *big.Int, currentAmount *big.Int) error {

	d.blockBalanceCaches[from] = currentAmount

	fromOriginRIndex := new(big.Int).Div(originAmount, d.poolInfo.GetRangeInterval()).Uint64()
	fromCurrentRIndex := new(big.Int).Div(currentAmount, d.poolInfo.GetRangeInterval()).Uint64()

	if fromOriginRIndex > d.poolInfo.GetRangeCount().Uint64()-1 {
		fromOriginRIndex = d.poolInfo.GetRangeCount().Uint64() - 1
	}

	if fromCurrentRIndex > d.poolInfo.GetRangeCount().Uint64()-1 {
		fromCurrentRIndex = d.poolInfo.GetRangeCount().Uint64() - 1
	}

	if fromOriginRIndex != fromCurrentRIndex {
		fromOriginRangeInfo := d.GetRangeInfo(fromOriginRIndex)
		fromCurrentRangeInfo := d.GetRangeInfo(fromCurrentRIndex)

		if fromOriginRangeInfo.totalCount.Cmp(common.Big0) > 0 {
			fromOriginRangeInfo.totalCount = new(big.Int).Sub(fromOriginRangeInfo.totalCount, common.Big1)
		}
		fromCurrentRangeInfo.totalCount = new(big.Int).Add(fromCurrentRangeInfo.totalCount, common.Big1)
		d.SetRangeInfo(fromOriginRIndex, fromOriginRangeInfo)
		d.SetRangeInfo(fromCurrentRIndex, fromCurrentRangeInfo)

		for _, rewardToken := range d.poolInfo.GetRewardTokens() {
			userInfo := d.farmContract.GetUserInfo(d.poolAddress, from)
			rewardInfo := userInfo.GetHolderRewardInfo(rewardToken)

			fromOriginRangePerShare := d.GetHolderRewardPerShare(rewardToken, fromOriginRIndex)
			fromCurrentRangePerShare := d.GetHolderRewardPerShare(rewardToken, fromCurrentRIndex)

			currentReward := new(big.Int).Add(rewardInfo.Reward, new(big.Int).Sub(fromOriginRangePerShare, rewardInfo.RewardDebt))
			currentRewardDebt := fromCurrentRangePerShare

			if d.isFork0815 && currentReward.Cmp(big.NewInt(0)) < 0 {
				currentReward = big.NewInt(0)
			}

			userInfo.SetHolderRewardInfo(rewardToken, currentReward, currentRewardDebt)
		}
	} else {
		// Genesis Address balance range info handled
		fromOriginRangeInfo := d.GetRangeInfo(fromOriginRIndex)
		if fromOriginRangeInfo.totalCount.Cmp(common.Big0) == 0 {
			fromOriginRangeInfo.totalCount = big.NewInt(1)
			d.SetRangeInfo(fromOriginRIndex, fromOriginRangeInfo)
		}
	}

	if err := d.updateAchievement(from, originAmount, currentAmount); err != nil {
		return err
	}

	return nil
}

func (d *PoolDistribution) updateAchievement(from common.Address, originAmount *big.Int, currentAmount *big.Int) error {

	start := time.Now()
	parentCount := 0

	if BenchMarkPrint {
		fmt.Printf("\n")
		fmt.Printf("------------------------------------------------------------------------------------------------\n")
		fmt.Printf("-                   PoolDistribution.updateAchievement BenchMarkTest Logs                      -\n")
		fmt.Printf("------------------------------------------------------------------------------------------------\n")
	}

	forFathersList := make([]common.Address, TreeHeightMaxLimit)
	parent := from
	for i := 0; i < len(forFathersList) && parent != common.HexToAddress(NullAddress) && parent != common.HexToAddress(BurnAddress); i++ {
		forFathersList[i] = parent
		parent = d.addressTreeContract.ParentOf(parent)
		parentCount++
	}

	if BenchMarkPrint {
		fmt.Printf("- Get Forfathers\t time:%dms\tparent count:%d\n", time.Since(start).Milliseconds(), parentCount)
	}

	__cpuTimes := map[string]int64{}
	for childIndex := 0; childIndex < len(forFathersList)-1; childIndex++ {
		child := forFathersList[childIndex]
		parent = forFathersList[childIndex+1]
		if parent == common.HexToAddress(NullAddress) || parent == common.HexToAddress(BurnAddress) {
			break
		}

		__userInfoOfStart := time.Now()
		parentInfo := d.farmContract.GetUserInfo(d.poolAddress, parent)
		__cpuTimes["userInfoOf"] += time.Since(__userInfoOfStart).Microseconds()

		__childrenOfStart := time.Now()
		children := d.addressTreeContract.ChildrenOf(parent)
		__cpuTimes["childrenOf"] += time.Since(__childrenOfStart).Microseconds()

		childrenHolds := parentInfo.GetChildrenHoldAmount()
		if len(childrenHolds) < len(*children) {
			diff := len(*children) - len(childrenHolds)
			for i := 0; i < diff; i++ {
				childrenHolds = append(childrenHolds, big.NewInt(0))
			}
		}

		// Add origin amount
		for i, c := range *children {
			if c == child {
				childrenHolds[i] = new(big.Int).Add(currentAmount, new(big.Int).Sub(childrenHolds[i], originAmount))
				break
			}
		}

		__innerLoopStart := time.Now()
		for _, rewardToken := range d.poolInfo.GetRewardTokens() {
			rewardInfo := parentInfo.GetCommunityRewardInfo(rewardToken)
			communityPerShare := d.farmContract.GetCommunityAccRewardPerShare(d.poolAddress, rewardToken)

			rewardInfo.Reward = new(big.Int).Add(
				rewardInfo.Reward,
				new(big.Int).Mul(
					parentInfo.GetCommunityPower(),
					new(big.Int).Sub(
						communityPerShare,
						rewardInfo.RewardDebt,
					),
				),
			)

			rewardInfo.RewardDebt = communityPerShare
			parentInfo.SetCommunityRewardInfo(
				rewardToken,
				rewardInfo.Reward,
				rewardInfo.RewardDebt,
			)
		}
		__cpuTimes["innerLoop"] += time.Since(__innerLoopStart).Microseconds()

		__currentCommunityPower := time.Now()
		currentActivePower := communityPower(&childrenHolds)
		__cpuTimes["currentCommunityPower"] += time.Since(__currentCommunityPower).Microseconds()

		__commitStart := time.Now()
		d.poolInfo.SetCommunityTotalPower(
			new(big.Int).Sub(
				new(big.Int).Add(
					d.poolInfo.GetCommunityTotalPower(),
					currentActivePower,
				),
				parentInfo.GetCommunityPower(),
			),
		)
		parentInfo.SetChildrenHoldAmount(childrenHolds)
		parentInfo.SetCommunityPower(currentActivePower)
		__cpuTimes["commit"] += time.Since(__commitStart).Microseconds()
	}

	if BenchMarkPrint {
		fmt.Printf("---- GetUserInfoOf\t time:%.2f ms\n", float32(__cpuTimes["userInfoOf"])/1000)
		fmt.Printf("---- ChildrenOf\t\t time:%.2f ms\n", float32(__cpuTimes["childrenOf"])/1000)
		fmt.Printf("---- InnerLoop\t\t time:%.2f ms\n", float32(__cpuTimes["innerLoop"])/1000)
		fmt.Printf("---- CommunityPower\t time:%.2f ms\n", float32(__cpuTimes["currentCommunityPower"])/1000)
		fmt.Printf("---- ParentInfo.Commit\t time:%.2f ms\n", float32(__cpuTimes["commit"])/1000)
		fmt.Printf("- UpdateAchievement\t time:%d ms\n", time.Since(start).Milliseconds())
		fmt.Printf("\n")
		start = time.Now()
	}

	return nil
}

func communityPower(holdAmounts *[]*big.Int) *big.Int {

	maxHoldAmount := uint64(0)
	maxPowerNumber := uint64(0)
	total := uint64(0)

	for _, childAmount := range *holdAmounts {
		childAmountEther := new(big.Int).Div(childAmount, big.NewInt(params.Ether)).Uint64()

		powerNumber := uint64(0)
		if childAmountEther < ActivePowerMultipleMaxLimit {
			// < 10000 : *10
			powerNumber = new(big.Int).Div(
				new(big.Int).Mul(childAmount, big.NewInt(10)),
				big.NewInt(params.Ether),
			).Uint64()
		} else {
			// >= 10000 : + 90000
			powerNumber = childAmountEther + 90000
		}

		if powerNumber > maxPowerNumber {
			maxPowerNumber = powerNumber
			maxHoldAmount = childAmountEther
		}
		total += powerNumber
	}

	total -= maxPowerNumber
	cbrt := math.Cbrt(float64(maxHoldAmount))
	if cbrt > 0 {
		total += uint64(cbrt)
	}

	return new(big.Int).SetUint64(total)
}
