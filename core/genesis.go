// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/core/anchor_network"
	"github.com/ethereum/go-ethereum/node"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

//go:generate gencodec -type Genesis -field-override genesisSpecMarshaling -out gen_genesis.go
//go:generate gencodec -type GenesisAccount -field-override genesisAccountMarshaling -out gen_genesis_account.go

var errGenesisNoConfig = errors.New("genesis has no chain configuration")

// Genesis specifies the header fields, state of a genesis block. It also defines hard
// fork switch-over blocks through the chain configuration.
type Genesis struct {
	Config     *params.ChainConfig `json:"config"`
	Nonce      uint64              `json:"nonce"`
	Timestamp  uint64              `json:"timestamp"`
	ExtraData  []byte              `json:"extraData"`
	GasLimit   uint64              `json:"gasLimit"   gencodec:"required"`
	Difficulty *big.Int            `json:"difficulty" gencodec:"required"`
	Mixhash    common.Hash         `json:"mixHash"`
	Coinbase   common.Address      `json:"coinbase"`
	Alloc      GenesisAlloc        `json:"alloc"      gencodec:"required"`

	// These fields are used for consensus tests. Please don't use them
	// in actual genesis blocks.
	Number     uint64      `json:"number"`
	GasUsed    uint64      `json:"gasUsed"`
	ParentHash common.Hash `json:"parentHash"`
	BaseFee    *big.Int    `json:"baseFeePerGas"`
}

// GenesisAlloc specifies the initial state that is part of the genesis block.
type GenesisAlloc map[common.Address]GenesisAccount

func (ga *GenesisAlloc) UnmarshalJSON(data []byte) error {
	m := make(map[common.UnprefixedAddress]GenesisAccount)
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ga = make(GenesisAlloc)
	for addr, a := range m {
		(*ga)[common.Address(addr)] = a
	}
	return nil
}

// GenesisAccount is an account in the state of the genesis block.
type GenesisAccount struct {
	Code       []byte                      `json:"code,omitempty"`
	Storage    map[common.Hash]common.Hash `json:"storage,omitempty"`
	Balance    *big.Int                    `json:"balance" gencodec:"required"`
	Nonce      uint64                      `json:"nonce,omitempty"`
	PrivateKey []byte                      `json:"secretKey,omitempty"` // for tests
}

// field type overrides for gencodec
type genesisSpecMarshaling struct {
	Nonce      math.HexOrDecimal64
	Timestamp  math.HexOrDecimal64
	ExtraData  hexutil.Bytes
	GasLimit   math.HexOrDecimal64
	GasUsed    math.HexOrDecimal64
	Number     math.HexOrDecimal64
	Difficulty *math.HexOrDecimal256
	BaseFee    *math.HexOrDecimal256
	Alloc      map[common.UnprefixedAddress]GenesisAccount
}

type genesisAccountMarshaling struct {
	Code       hexutil.Bytes
	Balance    *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	Storage    map[storageJSON]storageJSON
	PrivateKey hexutil.Bytes
}

// storageJSON represents a 256 bit byte array, but allows less than 256 bits when
// unmarshaling from hex.
type storageJSON common.Hash

func (h *storageJSON) UnmarshalText(text []byte) error {
	text = bytes.TrimPrefix(text, []byte("0x"))
	if len(text) > 64 {
		return fmt.Errorf("too many hex characters in storage key/value %q", text)
	}
	offset := len(h) - len(text)/2 // pad on the left
	if _, err := hex.Decode(h[offset:], text); err != nil {
		fmt.Println(err)
		return fmt.Errorf("invalid hex storage key/value %q", text)
	}
	return nil
}

func (h storageJSON) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

// GenesisMismatchError is raised when trying to overwrite an existing
// genesis block with an incompatible one.
type GenesisMismatchError struct {
	Stored, New common.Hash
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("database contains incompatible genesis (have %x, new %x)", e.Stored, e.New)
}

// SetupGenesisBlock writes or updates the genesis block in db.
// The block that will be used is:
//
//	                     genesis == nil       genesis != nil
//	                  +------------------------------------------
//	db has no genesis |  main-net default  |  genesis
//	db has genesis    |  from DB           |  genesis (if compatible)
//
// The stored chain configuration will be updated if it is compatible (i.e. does not
// specify a fork block below the local head block). In case of a conflict, the
// error is a *params.ConfigCompatError and the new, unwritten config is returned.
//
// The returned chain configuration is never nil.
func SetupGenesisBlock(db ethdb.Database, genesis *Genesis) (*params.ChainConfig, common.Hash, error) {
	return SetupGenesisBlockWithOverride(db, genesis, nil, nil, nil)
}

func SetupGenesisBlockWithOverride(db ethdb.Database, genesis *Genesis, overrideBerlin, overrideArrowGlacier, overrideTerminalTotalDifficulty *big.Int) (*params.ChainConfig, common.Hash, error) {
	if genesis != nil && genesis.Config == nil {
		return params.AllEthashProtocolChanges, common.Hash{}, errGenesisNoConfig
	}
	// Just commit the new block if there is no stored genesis block.
	stored := rawdb.ReadCanonicalHash(db, 0)
	systemcontracts.GenesisHash = stored
	if (stored == common.Hash{}) {
		if genesis == nil {
			log.Info("Writing default main-net genesis block")
			genesis = DefaultGenesisBlock()
		} else {
			log.Info("Writing custom genesis block")
		}
		block, err := genesis.Commit(db)
		if err != nil {
			return genesis.Config, common.Hash{}, err
		}
		return genesis.Config, block.Hash(), nil
	}
	// We have the genesis block in database(perhaps in ancient database)
	// but the corresponding state is missing.
	header := rawdb.ReadHeader(db, stored, 0)
	if _, err := state.New(header.Root, state.NewDatabaseWithConfigAndCache(db, nil), nil); err != nil {
		if genesis == nil {
			genesis = DefaultGenesisBlock()
		}
		// Ensure the stored genesis matches with the given one.
		hash := genesis.ToBlock(nil).Hash()
		if hash != stored {
			return genesis.Config, hash, &GenesisMismatchError{stored, hash}
		}
		block, err := genesis.Commit(db)
		if err != nil {
			return genesis.Config, hash, err
		}
		return genesis.Config, block.Hash(), nil
	}
	// Check whether the genesis block is already written.
	if genesis != nil {
		hash := genesis.ToBlock(nil).Hash()
		if hash != stored {
			return genesis.Config, hash, &GenesisMismatchError{stored, hash}
		}
	}
	// Get the existing chain configuration.
	newcfg := genesis.configOrDefault(stored)
	if overrideBerlin != nil {
		newcfg.BerlinBlock = overrideBerlin
	}
	if overrideArrowGlacier != nil {
		newcfg.ArrowGlacierBlock = overrideArrowGlacier
	}
	if overrideTerminalTotalDifficulty != nil {
		newcfg.TerminalTotalDifficulty = overrideTerminalTotalDifficulty
	}
	if err := newcfg.CheckConfigForkOrder(); err != nil {
		return newcfg, common.Hash{}, err
	}
	storedcfg := rawdb.ReadChainConfig(db, stored)
	if storedcfg == nil {
		log.Warn("Found genesis block without chain config")
		rawdb.WriteChainConfig(db, stored, newcfg)
		return newcfg, stored, nil
	}
	// Special case: don't change the existing config of a non-mainnet chain if no new
	// config is supplied. These chains would get AllProtocolChanges (and a compat error)
	// if we just continued here.
	// The full node of two testnets may run without genesis file after been inited.
	if genesis == nil && stored != params.PDANetGenesisHash && stored != params.TestnetGenesisHash {
		return storedcfg, stored, nil
	}
	// Check config compatibility and write the config. Compatibility errors
	// are returned to the caller unless we're already at block zero.
	height := rawdb.ReadHeaderNumber(db, rawdb.ReadHeadHeaderHash(db))
	if height == nil {
		return newcfg, stored, fmt.Errorf("missing block number for head header hash")
	}
	compatErr := storedcfg.CheckCompatible(newcfg, *height)
	if compatErr != nil && *height != 0 && compatErr.RewindTo != 0 {
		return newcfg, stored, compatErr
	}
	rawdb.WriteChainConfig(db, stored, newcfg)
	return newcfg, stored, nil
}

func (g *Genesis) configOrDefault(ghash common.Hash) *params.ChainConfig {
	switch {
	case g != nil:
		return g.Config

	case ghash == params.PDANetGenesisHash:
		return params.PDANetChainConfig

	case ghash == params.TestnetGenesisHash:
		return params.TestNetChainConfig

	default:
		return params.AllEthashProtocolChanges
	}
}

// ToBlock creates the genesis block and writes state of a genesis specification
// to the given database (or discards it if nil).
func (g *Genesis) ToBlock(db ethdb.Database) *types.Block {
	if db == nil {
		db = rawdb.NewMemoryDatabase()
	}
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	if err != nil {
		panic(err)
	}
	for addr, account := range g.Alloc {
		statedb.AddBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	root := statedb.IntermediateRoot(false)
	head := &types.Header{
		Number:     new(big.Int).SetUint64(g.Number),
		Nonce:      types.EncodeNonce(g.Nonce),
		Time:       g.Timestamp,
		ParentHash: g.ParentHash,
		Extra:      g.ExtraData,
		GasLimit:   g.GasLimit,
		GasUsed:    g.GasUsed,
		BaseFee:    g.BaseFee,
		Difficulty: g.Difficulty,
		MixDigest:  g.Mixhash,
		Coinbase:   g.Coinbase,
		Root:       root,
	}
	if g.GasLimit == 0 {
		head.GasLimit = params.GenesisGasLimit
	}
	if g.Difficulty == nil && g.Mixhash == (common.Hash{}) {
		head.Difficulty = params.GenesisDifficulty
	}
	if g.Config != nil && g.Config.IsLondon(common.Big0) {
		if g.BaseFee != nil {
			head.BaseFee = g.BaseFee
		} else {
			head.BaseFee = new(big.Int).SetUint64(params.InitialBaseFee)
		}
	}
	statedb.Commit(nil)
	statedb.Database().TrieDB().Commit(root, true, nil)

	return types.NewBlock(head, nil, nil, nil, trie.NewStackTrie(nil))
}

// Commit writes the block and state of a genesis specification to the database.
// The block is committed as the canonical head block.
func (g *Genesis) Commit(db ethdb.Database) (*types.Block, error) {
	block := g.ToBlock(db)
	if block.Number().Sign() != 0 {
		return nil, errors.New("can't commit genesis block with number > 0")
	}
	config := g.Config
	if config == nil {
		config = params.AllEthashProtocolChanges
	}
	if err := config.CheckConfigForkOrder(); err != nil {
		return nil, err
	}
	if config.Clique != nil && len(block.Extra()) == 0 {
		return nil, errors.New("can't start clique chain without signers")
	}
	rawdb.WriteTd(db, block.Hash(), block.NumberU64(), block.Difficulty())
	rawdb.WriteBlock(db, block)
	rawdb.WriteReceipts(db, block.Hash(), block.NumberU64(), nil)
	rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(db, block.Hash())
	rawdb.WriteHeadFastBlockHash(db, block.Hash())
	rawdb.WriteHeadHeaderHash(db, block.Hash())
	rawdb.WriteChainConfig(db, block.Hash(), config)
	return block, nil
}

// MustCommit writes the genesis block and state to db, panicking on error.
// The block is committed as the canonical head block.
func (g *Genesis) MustCommit(db ethdb.Database) *types.Block {
	block, err := g.Commit(db)
	if err != nil {
		panic(err)
	}
	return block
}

// GenesisBlockForTesting creates and writes a block in which addr has the given wei balance.
func GenesisBlockForTesting(db ethdb.Database, addr common.Address, balance *big.Int) *types.Block {
	g := Genesis{
		Alloc:   GenesisAlloc{addr: {Balance: balance}},
		BaseFee: big.NewInt(params.InitialBaseFee),
	}
	return g.MustCommit(db)
}

// DefaultGenesisBlock returns the Ethereum main net genesis block.
func DefaultGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.PDANetChainConfig,
		Nonce:      66,
		ExtraData:  hexutil.MustDecode("0x0000000000000000000000000000000000000000000000000000000000000000d419e1C6c2B010ADc3B66C94B4B702C9b73e5A7b31e6Ee03C6E771dB95F0f5EF51879C3cdB9f0a0232664779cB7A0F04E6F20e67A346660118E836B80000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   25000000,
		Difficulty: big.NewInt(1),
		Timestamp:  1717603200,
		Alloc: map[common.Address]GenesisAccount{
			common.HexToAddress(systemcontracts.AddressTreeContract):          {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.PDANetAddressTreeContractByteCode)},
			common.HexToAddress(systemcontracts.SystemDaoContract):            {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.PDANetSystemDaoContractByteCode)},
			common.HexToAddress(systemcontracts.FarmContract):                 {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.PDANetFarmContractByteCode)},
			common.HexToAddress(systemcontracts.PDANetGenesisDeployerAddress): {Balance: new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Ether))},
			common.HexToAddress(systemcontracts.PDANetGenesisAddress):         {Balance: new(big.Int).Mul(big.NewInt(10000000000-10), big.NewInt(params.Ether))},
		},
	}
}

// DefaultTestNetGenesisBlock returns the TestNet network genesis block.
func DefaultTestNetGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.TestNetChainConfig,
		Nonce:      88,
		ExtraData:  hexutil.MustDecode("0x0000000000000000000000000000000000000000000000000000000000000000e7d5f3df6925f0cf14c26c8c8b567d866b2c2c82b5b594f172292e32e64d11853df6ad1379fe38c6a3dc6e591dc4f00c5a5101236f6ff65d4add3f6d0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   15000000,
		Difficulty: big.NewInt(1),
		Timestamp:  1730678400,
		Alloc: map[common.Address]GenesisAccount{
			common.HexToAddress(systemcontracts.AddressTreeContract):           {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.TestNetAddressTreeContractByteCode)},
			common.HexToAddress(systemcontracts.SystemDaoContract):             {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.TestNetSystemDaoContractByteCode)},
			common.HexToAddress(systemcontracts.FarmContract):                  {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.TestNetFarmContractByteCode)},
			common.HexToAddress(systemcontracts.TestNetGenesisAddress):         {Balance: new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Ether))},
			common.HexToAddress(systemcontracts.TestNetGenesisDeployerAddress): {Balance: new(big.Int).Mul(big.NewInt(10000000000-10), big.NewInt(params.Ether))},
		},
	}
}

func DefaultAnchorNetGenesisBlock(
	stack *node.Node,
	forkBlockTimestamp uint64,
	forkBlockHash common.Hash,
	ipcPath string,
	info anchor_network.AnchorNetworkInfo,
) *Genesis {
	var genesis = &Genesis{
		Config: params.AnchorNetChainConfig,
		Nonce:  88,
		ExtraData: hexutil.MustDecode("0x" +
			"0000000000000000000000000000000000000000000000000000000000000000" +
			info.GenesisAddress.Hex()[2:] +
			forkBlockHash.Hex()[2:] +
			"0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		),
		GasLimit:   15000000,
		Difficulty: big.NewInt(1),
		Timestamp:  forkBlockTimestamp,
		Alloc: map[common.Address]GenesisAccount{
			common.HexToAddress(systemcontracts.AddressTreeContract): {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.AnchorNetAddressTreeContractByteCode)},
			common.HexToAddress(systemcontracts.SystemDaoContract):   {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.AnchorNetSystemDaoContractByteCode)},
			common.HexToAddress(systemcontracts.FarmContract):        {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.AnchorNetFarmContractByteCode)},
			common.HexToAddress(systemcontracts.AnchorContract):      {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.AnchorNetAnchorContractByteCode)},
			info.ManagerAddress: {Balance: new(big.Int).Mul(big.NewInt(1000), big.NewInt(params.Ether))},
		},
	}

	//stack.ResolvePath("maindate")
	cacheDBPath := stack.ResolvePath("cachedate")
	rdb, err := rawdb.NewLevelDBDatabase(cacheDBPath, 0, 0, "eth/db/cache_data/", false)
	if err != nil {
		panic(err)
	}

	genesis.Config.ChainID = info.ChainID
	genesis.Config.Anchor.ForkBlockNumber = info.ForkBlockNumber.Uint64()
	genesis.Config.Anchor.IPCPath = ipcPath
	genesis.Config.Anchor.GenesisAddress = info.GenesisAddress
	genesis.Config.Anchor.ManagerAddress = info.ManagerAddress
	genesis.Config.Anchor.CacheDataBase = rdb

	return genesis
}

// DeveloperGenesisBlock returns the 'geth --dev' genesis block.
func DeveloperGenesisBlock(period uint64, gasLimit uint64) *Genesis {
	// Override the default period to the user requested one
	config := *params.DevNetChainConfig
	config.Parlia = &params.ParliaConfig{
		Period: period,
		Epoch:  config.Parlia.Epoch,
	}

	if gasLimit == 0 {
		gasLimit = 30000000
	}

	// Assemble and return the genesis with the precompiles and faucet pre-funded
	return &Genesis{
		Config:     &config,
		Nonce:      0,
		ExtraData:  hexutil.MustDecode("0x0000000000000000000000000000000000000000000000000000000000000000a8722bd91815ad3f8adbb27075576946a7b0938a0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   gasLimit,
		BaseFee:    big.NewInt(0),
		Difficulty: big.NewInt(1),
		Timestamp:  1722096000,
		Alloc: map[common.Address]GenesisAccount{
			common.HexToAddress(systemcontracts.AddressTreeContract):          {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.DevNetAddressTreeContractByteCode)},
			common.HexToAddress(systemcontracts.SystemDaoContract):            {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.DevNetSystemDaoContractByteCode)},
			common.HexToAddress(systemcontracts.FarmContract):                 {Balance: big.NewInt(0), Code: hexutil.MustDecode(systemcontracts.DevNetFarmContractByteCode)},
			common.HexToAddress(systemcontracts.DevNetGenesisAddress):         {Balance: new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Ether))},
			common.HexToAddress(systemcontracts.DevNetGenesisDeployerAddress): {Balance: new(big.Int).Mul(big.NewInt(10000000000-10), big.NewInt(params.Ether))},
		},
	}
}

func decodePrealloc(data string) GenesisAlloc {
	var p []struct{ Addr, Balance *big.Int }
	if err := rlp.NewStream(strings.NewReader(data), 0).Decode(&p); err != nil {
		panic(err)
	}
	ga := make(GenesisAlloc, len(p))
	for _, account := range p {
		ga[common.BigToAddress(account.Addr)] = GenesisAccount{Balance: account.Balance}
	}
	return ga
}
