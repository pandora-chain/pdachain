package anchor

import (
	"context"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/anchor_network"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/systemcontracts/anchor"
	"github.com/ethereum/go-ethereum/core/systemcontracts/parlia"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"math"
	"math/big"
	"strings"
)

type L2AnchorContract struct {
	address     common.Address
	l1AnchorAbi abi.ABI
	l2AnchorAbi abi.ABI
	cli         *ethclient.Client
	api         *ethapi.PublicBlockChainAPI
	chainConfig *params.ChainConfig // Chain config
}

type L1ExchangeTransaction struct {
	FromToken   common.Address
	FromAddress common.Address
	ToToken     common.Address
	ToAddress   common.Address
	Amount      *big.Int
}

type L2ExchangeTransaction struct {
	Index       *big.Int
	FromToken   common.Address
	FromAddress common.Address
	ToToken     common.Address
	ToAddress   common.Address
	Amount      *big.Int
}

type L2BrunProof struct {
	Index     *big.Int
	Hash      common.Hash
	Signature []byte
}

func getAnchorNetworkInfo(cli *ethclient.Client, chainConfig *params.ChainConfig) (*anchor_network.AnchorNetworkInfo, error) {
	networkAbi, err := abi.JSON(strings.NewReader(parlia.AnchorNetworksABI))

	method := "anchorNetworkOfChainID"
	data, err := networkAbi.Pack(method, chainConfig.ChainID)
	if err != nil {
		return nil, err
	}

	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.PDANetAnchorNetworksManagerContractAddress)
	result, err := cli.CallContract(
		context.TODO(),
		ethereum.CallMsg{
			From:     common.HexToAddress("0x0000000000000000000000000000000000000000"),
			To:       &toAddress,
			Gas:      uint64(math.MaxUint64 / 2),
			GasPrice: nil,
			Data:     msgData,
		},
		nil,
	)
	if err != nil {
		return nil, err
	}

	var info anchor_network.AnchorNetworkInfo
	if err := networkAbi.UnpackIntoInterface(&info, method, result); err != nil {
		return nil, err
	}

	return &info, nil
}

func NewAnchorContract(cli *ethclient.Client, localAPI *ethapi.PublicBlockChainAPI, config *params.ChainConfig) (*L2AnchorContract, error) {
	info, err := getAnchorNetworkInfo(cli, config)
	if err != nil {
		return nil, err
	}

	l1abi, err := abi.JSON(strings.NewReader(parlia.AnchorABI))
	if err != nil {
		panic(err)
	}

	l2abi, err := abi.JSON(strings.NewReader(anchor.AnchorABI))
	if err != nil {
		panic(err)
	}

	return &L2AnchorContract{
		address:     info.AnchorContract,
		l1AnchorAbi: l1abi,
		l2AnchorAbi: l2abi,
		cli:         cli,
		api:         localAPI,
		chainConfig: config,
	}, nil
}

func (l2c *L2AnchorContract) l1ExchangesOfBlockNumber(l1BlockNumber uint64) (*[]L1ExchangeTransaction, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	blkNum := big.NewInt(0).SetUint64(l1BlockNumber)

	method := "exchangesOfBlockNumber"
	data, err := l2c.l1AnchorAbi.Pack(method, blkNum)
	if err != nil {
		panic(err)
	}

	// call
	msgData := (hexutil.Bytes)(data)
	result, err := l2c.cli.CallContract(
		ctx,
		ethereum.CallMsg{
			From:     common.HexToAddress("0x0000000000000000000000000000000000000000"),
			To:       &l2c.address,
			Gas:      uint64(math.MaxUint64 / 2),
			GasPrice: nil,
			Data:     msgData,
		},
		nil,
	)
	if err != nil {
		return nil, err
	}

	var ret0 []L1ExchangeTransaction

	if result != nil && len(result) == 0 {
		return &ret0, nil
	}

	if err := l2c.l1AnchorAbi.UnpackIntoInterface(&ret0, method, result); err != nil {
		return nil, err
	}
	return &ret0, nil
}

func mustNewType(solidityType string) abi.Type {
	tp, err := abi.NewType(solidityType, "", nil)
	if err != nil {
		panic(err)
	}
	return tp
}

func (l2c *L2AnchorContract) l2BurnProofs(blockHash common.Hash, coinBase common.Address, signFn SignTextFn) (proofs *[]L2BrunProof, err error) {
	if coinBase != l2c.chainConfig.Anchor.GenesisAddress {
		return nil, nil
	}

	reqNonce, err := l2c.l2RequestNonce(blockHash)
	if err != nil {
		return nil, err
	}

	prfNonce, err := l2c.l2ProofNonce(blockHash)
	if err != nil {
		return nil, err
	}

	// Index       *big.Int
	// FromToken   common.Address
	// FromAddress common.Address
	// ToToken     common.Address
	// ToAddress   common.Address
	// Amount      *big.Int
	arguments := abi.Arguments{
		{Type: mustNewType("uint256")},
		{Type: mustNewType("address")},
		{Type: mustNewType("address")},
		{Type: mustNewType("address")},
		{Type: mustNewType("address")},
		{Type: mustNewType("uint256")},
	}

	var prfs []L2BrunProof
	for i := prfNonce.Uint64(); i < reqNonce.Uint64() && i < 32; i++ {
		brunReq, err := l2c.l2BurnTransaction(blockHash, i)
		if err != nil {
			return nil, err
		}

		encodeData, err := arguments.Pack(
			brunReq.Index,
			brunReq.FromToken,
			brunReq.FromAddress,
			brunReq.ToToken,
			brunReq.ToAddress,
			brunReq.Amount,
		)
		if err != nil {
			return nil, err
		}

		brunReqHash := crypto.Keccak256Hash(encodeData)
		signature, err := signFn(accounts.Account{Address: coinBase}, brunReqHash.Bytes())
		if err != nil {
			return nil, err
		}
		signature[64] += 27

		prfs = append(prfs, L2BrunProof{
			Index:     big.NewInt(0).SetUint64(i),
			Hash:      brunReqHash,
			Signature: signature,
		})
	}
	return &prfs, nil
}

func (l2c *L2AnchorContract) l2ProofNonce(blockHash common.Hash) (*big.Int, error) {
	// block
	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	data, err := l2c.l2AnchorAbi.Pack("proofNonce")
	if err != nil {
		return nil, err
	}

	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.AnchorContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := l2c.api.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, blockNr, nil)
	if err != nil {
		return nil, err
	}

	var ret0 *big.Int
	if err := l2c.l2AnchorAbi.UnpackIntoInterface(&ret0, "proofNonce", result); err != nil {
		return nil, err
	}
	return ret0, nil
}

func (l2c *L2AnchorContract) l2RequestNonce(blockHash common.Hash) (*big.Int, error) {
	// block
	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	data, err := l2c.l2AnchorAbi.Pack("requestNonce")
	if err != nil {
		return nil, err
	}

	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.AnchorContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := l2c.api.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, blockNr, nil)
	if err != nil {
		return nil, err
	}

	var ret0 *big.Int
	if err := l2c.l2AnchorAbi.UnpackIntoInterface(&ret0, "requestNonce", result); err != nil {
		return nil, err
	}
	return ret0, nil
}

func (l2c *L2AnchorContract) l2BurnTransaction(blockHash common.Hash, index uint64) (*L2ExchangeTransaction, error) {
	// block
	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	data, err := l2c.l2AnchorAbi.Pack("burnRequestOf", big.NewInt(0).SetUint64(index))
	if err != nil {
		return nil, err
	}

	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.AnchorContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := l2c.api.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, blockNr, nil)
	if err != nil {
		return nil, err
	}

	var ret0 L2ExchangeTransaction
	if err := l2c.l2AnchorAbi.UnpackIntoInterface(&ret0, "burnRequestOf", result); err != nil {
		return nil, err
	}
	return &ret0, nil
}

//func (l2c *L2AnchorContract) l2ExchangeProof(blockHash common.Hash, index uint64) (*L2ExchangeTransaction, error) {
//	// block
//	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel() // cancel when we are finished consuming integers
//
//	data, err := l2c.l2AnchorAbi.Pack("burnProofOf", index)
//	if err != nil {
//		return nil, err
//	}
//
//	// call
//	msgData := (hexutil.Bytes)(data)
//	toAddress := common.HexToAddress(systemcontracts.AnchorContract)
//	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
//	result, err := l2c.api.Call(ctx, ethapi.TransactionArgs{
//		Gas:  &gas,
//		To:   &toAddress,
//		Data: &msgData,
//	}, blockNr, nil)
//	if err != nil {
//		return nil, err
//	}
//
//	var ret0 L2ExchangeTransaction
//	if err := l2c.l2AnchorAbi.UnpackIntoInterface(&ret0, "burnProofOf", result); err != nil {
//		return nil, err
//	}
//	return &ret0, nil
//}
