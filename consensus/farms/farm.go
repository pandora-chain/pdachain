package farms

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/farms/contract"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"strings"
)

const (
	DefaultMakeRelationTransferValue = 4
)

type Farm struct {
	contractAddress     common.Address
	state               *state.StateDB
	ethAPI              *ethapi.PublicBlockChainAPI
	addressTreeContract *contract.AddressTreeContract
	farmContract        *contract.FarmContract
	farmABI             abi.ABI
	transferABI         abi.ABI
	rTransferValue      *big.Int
}

type DistributeRewardEvent struct {
	poolAddress        common.Address
	rewardTokenAddress common.Address
	holderReward       *big.Int
	communityReward    *big.Int
}

type ERC20TransferEvent struct {
	from   common.Address
	to     common.Address
	amount *big.Int
}

func NewFarm(state *state.StateDB, ethAPI *ethapi.PublicBlockChainAPI, farmContractAddress common.Address, addressTreeContractAddress common.Address, makeNodeValue *big.Int) *Farm {

	farmABI, err := abi.JSON(strings.NewReader(systemcontracts.FarmABI))
	if err != nil {
		panic(err)
	}

	transferABI, err := abi.JSON(strings.NewReader(`[{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"from","type":"address"},{"indexed":true,"internalType":"address","name":"to","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Transfer","type":"event"}]`))
	if err != nil {
		panic(err)
	}

	if makeNodeValue.Cmp(big.NewInt(0)) == 0 {
		makeNodeValue = new(big.Int).Mul(big.NewInt(DefaultMakeRelationTransferValue), big.NewInt(params.Ether))
	}

	farm := &Farm{
		contractAddress:     farmContractAddress,
		state:               state,
		ethAPI:              ethAPI,
		addressTreeContract: contract.NewAddressTreeContract(state, addressTreeContractAddress),
		farmContract:        contract.NewFarmContract(state, farmContractAddress),
		farmABI:             farmABI,
		transferABI:         transferABI,
		rTransferValue:      makeNodeValue,
	}

	return farm
}

func (f *Farm) FinalizeBlock(chain core.ChainContext, chainConfig *params.ChainConfig, header *types.Header, txs *[]*types.Transaction, receipts *[]*types.Receipt, isFork0815 bool) error {

	poolTokens := f.farmContract.GetPools()
	poolInfos := map[common.Address]*contract.PoolInfo{}
	poolHolderDistributions := map[common.Address]*PoolDistribution{}

	snap := f.state.Snapshot()
	for _, token := range poolTokens {
		poolInfos[token] = f.farmContract.GetPoolInfo(token)
	}

	// Handle And Create AddressTree Node / Community Token Transfer EventLog
	signer := types.MakeSigner(chainConfig, header.Number)
	for _, receipt := range *receipts {
		if receipt.Status == types.ReceiptStatusFailed {
			continue
		}

		tx := (*txs)[receipt.TransactionIndex]
		parent, err := types.Sender(signer, tx)
		if err != nil {
			f.state.RevertToSnapshot(snap)
			log.Warn("FarmHandleBlock - ECRecover Sender Error", "number", header.Number, "hash", header.Hash())
			return err
		}
		child := tx.To()

		if child != nil && f.addressTreeContract.DepthOf(*child).Cmp(common.Big0) == 0 && f.state.GetCodeSize(parent) == 0 && f.state.GetCodeSize(*child) == 0 && tx.Value().Cmp(f.rTransferValue) >= 0 {
			if f.addressTreeContract.DepthOf(parent).Cmp(common.Big0) > 0 {
				if err := f.addressTreeContract.MakeRelation(chain, header, chainConfig, parent, *child); err != nil {
					f.state.RevertToSnapshot(snap)
					log.Warn("FarmHandleBlock - HandleAddressTree Error", "number", header.Number, "hash", header.Hash())
					return err
				}

				if err := f.addressTreeContract.AppendChild(parent, *child); err != nil {
					f.state.RevertToSnapshot(snap)
					log.Warn("FarmHandleBlock - HandleAddressTree Error", "number", header.Number, "hash", header.Hash())
					return err
				}

				for poolAddress, info := range poolInfos {
					if poolHolderDistributions[poolAddress] == nil {
						poolHolderDistributions[poolAddress] = newTokenHolderDistribution(f.state, f.ethAPI, f.farmContract, f.addressTreeContract, poolAddress, info, isFork0815)
					}
					dst := poolHolderDistributions[poolAddress]
					if balance, err := dst.balanceOf(header.ParentHash, *child); err != nil {
						f.state.RevertToSnapshot(snap)
						log.Warn("FarmHandleBlock - HandleImportRelationCall Error", "number", header.Number, "hash", header.Hash())
						return err
					} else {
						if balance.Cmp(big.NewInt(0)) > 0 {
							if err := dst.updateAchievement(*child, big.NewInt(0), balance); err != nil {
								f.state.RevertToSnapshot(snap)
								log.Warn("FarmHandleBlock - HandleUpdateAchievement Error", "number", header.Number, "hash", header.Hash())
								return err
							}
						}
					}
				}
			}
		}

		// has transaction call address contract importRelation methods
		isImportRelationCall := f.addressTreeContract.IsImportTransaction(tx)
		for _, l := range receipt.Logs {
			// keccak256("Transfer(address,address,uint256)")
			if l.Topics[0] == common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef") {
				if p := poolInfos[l.Address]; p != nil {
					if poolHolderDistributions[l.Address] == nil {
						poolHolderDistributions[l.Address] = newTokenHolderDistribution(f.state, f.ethAPI, f.farmContract, f.addressTreeContract, l.Address, p, isFork0815)
					}
					dst := poolHolderDistributions[l.Address]
					if err := dst.putTransferEventLog(
						header.ParentHash,
						common.BytesToAddress(l.Topics[1].Bytes()),
						common.BytesToAddress(l.Topics[2].Bytes()),
						new(big.Int).SetBytes(l.Data),
					); err != nil {
						f.state.RevertToSnapshot(snap)
						log.Warn("FarmHandleBlock - HandlePutCommunityTokenTransferEvent Error", "number", header.Number, "hash", header.Hash())
						return err
					}
				}
			} else if isImportRelationCall && f.addressTreeContract.IsAddressAddedLog(l) {
				parent := common.BytesToAddress(l.Topics[1].Bytes())
				child := common.BytesToAddress(l.Topics[2].Bytes())
				_ = f.addressTreeContract.AppendChild(parent, child)

				for poolAddress, info := range poolInfos {
					if poolHolderDistributions[poolAddress] == nil {
						poolHolderDistributions[poolAddress] = newTokenHolderDistribution(f.state, f.ethAPI, f.farmContract, f.addressTreeContract, poolAddress, info, isFork0815)
					}
					dst := poolHolderDistributions[poolAddress]
					if balance, err := dst.balanceOf(header.ParentHash, child); err != nil {
						f.state.RevertToSnapshot(snap)
						log.Warn("FarmHandleBlock - HandleImportRelationCall Error", "number", header.Number, "hash", header.Hash())
						return err
					} else {
						if balance.Cmp(big.NewInt(0)) > 0 {
							if err := dst.updateAchievement(child, big.NewInt(0), balance); err != nil {
								f.state.RevertToSnapshot(snap)
								log.Warn("FarmHandleBlock - HandleUpdateAchievement Error", "number", header.Number, "hash", header.Hash())
								return err
							}
						}
					}
				}
			} else if l.Address == common.HexToAddress(systemcontracts.FarmContract) && l.Topics[0] == f.farmABI.Events["DistributeRewards"].ID {
				poolAddress := common.BytesToAddress(l.Topics[1].Bytes())
				rewardToken := common.BytesToAddress(l.Topics[2].Bytes())
				holderReward := new(big.Int).SetBytes(l.Data[0:32])
				communityReward := new(big.Int).SetBytes(l.Data[32:64])

				if poolHolderDistributions[poolAddress] == nil {
					poolHolderDistributions[poolAddress] = newTokenHolderDistribution(f.state, f.ethAPI, f.farmContract, f.addressTreeContract, poolAddress, poolInfos[poolAddress], isFork0815)
				}
				dst := poolHolderDistributions[poolAddress]
				dst.UpdateRewardPerShares(rewardToken, holderReward, communityReward)
			}
		}
	}

	// Storage
	for _, dst := range poolHolderDistributions {
		dst.Storage()
	}
	return nil
}
