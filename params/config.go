// Copyright 2016 The go-ethereum Authors
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

package params

import (
	"encoding/binary"
	"fmt"
	"github.com/ethereum/go-ethereum/ethdb"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/sha3"
)

// Genesis hashes to enforce below configs on.
var (
	PDANetGenesisHash  = common.HexToHash("0x838cd9ab70ecd98eea94ef49cebbb5c0d0f5195de2e8f30ddc5d57815ac27512")
	TestnetGenesisHash = common.HexToHash("0x70fccba131b1ea1286df2bac029657a9f6346a9b1d776a8dbcd0ad6edd768bf6")
)

// TrustedCheckpoints associates each known checkpoint with the genesis hash of
// the chain it belongs to.
var TrustedCheckpoints = map[common.Hash]*TrustedCheckpoint{
	//MainnetGenesisHash: MainnetTrustedCheckpoint,
	//TestNetGenesisHash: RopstenTrustedCheckpoint,
}

// CheckpointOracles associates each known checkpoint oracles with the genesis hash of
// the chain it belongs to.
var CheckpointOracles = map[common.Hash]*CheckpointOracleConfig{
	//MainnetGenesisHash: MainnetCheckpointOracle,
	//TestNetGenesisHash: RopstenCheckpointOracle,
}

const (
	//Community fork, validator will be voted by sysytem validator contract
	CommunitySwapBlockMain = 2903050
	CommunitySwapBlockDev  = 82157 // dev net
	CommunitySwapBlockTest = 500   // test net

	//Validator fork, validator contract will be deployed to network
	DeployValidatorBlock     = 2608100
	DeployValidatorBlockDev  = 44828 // dev net
	DeployValidatorBlockTest = 100   // test net
)

var (
	// MainnetChainConfig is the chain parameters to run a node on the main network.
	PDANetChainConfig = &ChainConfig{
		ChainID:             big.NewInt(140705),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		RamanujanBlock:      big.NewInt(0),
		NielsBlock:          big.NewInt(0),
		MirrorSyncBlock:     big.NewInt(0),
		BrunoBlock:          big.NewInt(0),
		EulerBlock:          big.NewInt(0),
		GibbsBlock:          big.NewInt(0),
		NanoBlock:           big.NewInt(0),
		MoranBlock:          big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		Parlia: &ParliaConfig{
			Period: 5,
			Epoch:  1000,
		},
	}

	TestNetChainConfig = &ChainConfig{
		ChainID:             big.NewInt(20241104),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		RamanujanBlock:      big.NewInt(0),
		NielsBlock:          big.NewInt(0),
		MirrorSyncBlock:     big.NewInt(0),
		BrunoBlock:          big.NewInt(0),
		EulerBlock:          big.NewInt(0),
		GibbsBlock:          big.NewInt(0),
		NanoBlock:           big.NewInt(0),
		MoranBlock:          big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		Parlia: &ParliaConfig{
			Period: 5,
			Epoch:  1000,
		},
	}

	AnchorNetChainConfig = &ChainConfig{
		ChainID:             big.NewInt(250101),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		RamanujanBlock:      big.NewInt(0),
		NielsBlock:          big.NewInt(0),
		MirrorSyncBlock:     big.NewInt(0),
		BrunoBlock:          big.NewInt(0),
		EulerBlock:          big.NewInt(0),
		GibbsBlock:          big.NewInt(0),
		NanoBlock:           big.NewInt(0),
		MoranBlock:          big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		Anchor: &AnchorConfig{
			Period:                    1,
			Epoch:                     1000,
			WaitingConfirmBlockNumber: 720,
		},
	}

	DevNetChainConfig = &ChainConfig{
		ChainID:             big.NewInt(1337),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		RamanujanBlock:      big.NewInt(0),
		NielsBlock:          big.NewInt(0),
		MirrorSyncBlock:     big.NewInt(0),
		BrunoBlock:          big.NewInt(0),
		EulerBlock:          big.NewInt(0),
		GibbsBlock:          big.NewInt(0),
		NanoBlock:           big.NewInt(0),
		MoranBlock:          big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		Parlia: &ParliaConfig{
			Period: 3,
			Epoch:  1000,
		},
	}

	// AllEthashProtocolChanges contains every protocol change (EIPs) introduced
	// and accepted by the Ethereum core developers into the Ethash consensus.
	//
	// This configuration is intentionally not using keyed fields to force anyone
	// adding flags to the config to also have to set these fields.
	AllEthashProtocolChanges = &ChainConfig{
		big.NewInt(1337),
		big.NewInt(0),
		nil,
		false,
		big.NewInt(0),
		common.Hash{},
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		nil,
		nil,
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		new(EthashConfig),
		nil,
		nil,
		nil,
		nil,
	}

	// AllCliqueProtocolChanges contains every protocol change (EIPs) introduced
	// and accepted by the Ethereum core developers into the Clique consensus.
	//
	// This configuration is intentionally not using keyed fields to force anyone
	// adding flags to the config to also have to set these fields.
	AllCliqueProtocolChanges = &ChainConfig{
		big.NewInt(1337),
		big.NewInt(0),
		nil,
		false,
		big.NewInt(0),
		common.Hash{},
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		nil,
		nil,
		big.NewInt(0),
		nil,
		nil,
		nil,
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		nil,
		nil,
		nil,
		&CliqueConfig{Period: 0, Epoch: 30000},
		nil,
		nil,
		nil,
	}

	TestChainConfig = &ChainConfig{
		big.NewInt(1),
		big.NewInt(0),
		nil,
		false,
		big.NewInt(0),
		common.Hash{},
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		nil,
		nil,
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		nil,
		new(EthashConfig),
		nil,
		nil,
		nil,
		nil,
	}
	TestRules = TestChainConfig.Rules(new(big.Int), false)
)

// TrustedCheckpoint represents a set of post-processed trie roots (CHT and
// BloomTrie) associated with the appropriate section index and head hash. It is
// used to start light syncing from this checkpoint and avoid downloading the
// entire header chain while still being able to securely access old headers/logs.
type TrustedCheckpoint struct {
	SectionIndex uint64      `json:"sectionIndex"`
	SectionHead  common.Hash `json:"sectionHead"`
	CHTRoot      common.Hash `json:"chtRoot"`
	BloomRoot    common.Hash `json:"bloomRoot"`
}

// HashEqual returns an indicator comparing the itself hash with given one.
func (c *TrustedCheckpoint) HashEqual(hash common.Hash) bool {
	if c.Empty() {
		return hash == common.Hash{}
	}
	return c.Hash() == hash
}

// Hash returns the hash of checkpoint's four key fields(index, sectionHead, chtRoot and bloomTrieRoot).
func (c *TrustedCheckpoint) Hash() common.Hash {
	var sectionIndex [8]byte
	binary.BigEndian.PutUint64(sectionIndex[:], c.SectionIndex)

	w := sha3.NewLegacyKeccak256()
	w.Write(sectionIndex[:])
	w.Write(c.SectionHead[:])
	w.Write(c.CHTRoot[:])
	w.Write(c.BloomRoot[:])

	var h common.Hash
	w.Sum(h[:0])
	return h
}

// Empty returns an indicator whether the checkpoint is regarded as empty.
func (c *TrustedCheckpoint) Empty() bool {
	return c.SectionHead == (common.Hash{}) || c.CHTRoot == (common.Hash{}) || c.BloomRoot == (common.Hash{})
}

// CheckpointOracleConfig represents a set of checkpoint contract(which acts as an oracle)
// config which used for light client checkpoint syncing.
type CheckpointOracleConfig struct {
	Address   common.Address   `json:"address"`
	Signers   []common.Address `json:"signers"`
	Threshold uint64           `json:"threshold"`
}

// ChainConfig is the core config which determines the blockchain settings.
//
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig struct {
	ChainID *big.Int `json:"chainId"` // chainId identifies the current chain and is used for replay protection

	HomesteadBlock *big.Int `json:"homesteadBlock,omitempty" toml:",omitempty"` // Homestead switch block (nil = no fork, 0 = already homestead)

	DAOForkBlock   *big.Int `json:"daoForkBlock,omitempty" toml:",omitempty"`   // TheDAO hard-fork switch block (nil = no fork)
	DAOForkSupport bool     `json:"daoForkSupport,omitempty" toml:",omitempty"` // Whether the nodes supports or opposes the DAO hard-fork

	// EIP150 implements the Gas price changes (https://github.com/ethereum/EIPs/issues/150)
	EIP150Block *big.Int    `json:"eip150Block,omitempty"` // EIP150 HF block (nil = no fork)
	EIP150Hash  common.Hash `json:"eip150Hash,omitempty"`  // EIP150 HF hash (needed for header only clients as only gas pricing changed)

	EIP155Block *big.Int `json:"eip155Block,omitempty"` // EIP155 HF block
	EIP158Block *big.Int `json:"eip158Block,omitempty"` // EIP158 HF block

	ByzantiumBlock      *big.Int `json:"byzantiumBlock,omitempty"`      // Byzantium switch block (nil = no fork, 0 = already on byzantium)
	ConstantinopleBlock *big.Int `json:"constantinopleBlock,omitempty"` // Constantinople switch block (nil = no fork, 0 = already activated)
	PetersburgBlock     *big.Int `json:"petersburgBlock,omitempty"`     // Petersburg switch block (nil = same as Constantinople)
	IstanbulBlock       *big.Int `json:"istanbulBlock,omitempty"`       // Istanbul switch block (nil = no fork, 0 = already on istanbul)
	MuirGlacierBlock    *big.Int `json:"muirGlacierBlock,omitempty"`    // Eip-2384 (bomb delay) switch block (nil = no fork, 0 = already activated)
	BerlinBlock         *big.Int `json:"berlinBlock,omitempty"`         // Berlin switch block (nil = no fork, 0 = already on berlin)
	YoloV3Block         *big.Int `json:"yoloV3Block,omitempty"`         // YOLO v3: Gas repricings TODO @holiman add EIP references
	CatalystBlock       *big.Int `json:"catalystBlock,omitempty"`       // Catalyst switch block (nil = no fork, 0 = already on catalyst)
	LondonBlock         *big.Int `json:"londonBlock,omitempty"`         // London switch block (nil = no fork, 0 = already on london)
	ArrowGlacierBlock   *big.Int `json:"arrowGlacierBlock,omitempty"`   // Eip-4345 (bomb delay) switch block (nil = no fork, 0 = already activated)
	MergeForkBlock      *big.Int `json:"mergeForkBlock,omitempty"`      // EIP-3675 (TheMerge) switch block (nil = no fork, 0 = already in merge proceedings)
	// TerminalTotalDifficulty is the amount of total difficulty reached by
	// the network that triggers the consensus upgrade.
	TerminalTotalDifficulty *big.Int `json:"terminalTotalDifficulty,omitempty"`

	RamanujanBlock  *big.Int `json:"ramanujanBlock,omitempty" toml:",omitempty"`  // ramanujanBlock switch block (nil = no fork, 0 = already activated)
	NielsBlock      *big.Int `json:"nielsBlock,omitempty" toml:",omitempty"`      // nielsBlock switch block (nil = no fork, 0 = already activated)
	MirrorSyncBlock *big.Int `json:"mirrorSyncBlock,omitempty" toml:",omitempty"` // mirrorSyncBlock switch block (nil = no fork, 0 = already activated)
	BrunoBlock      *big.Int `json:"brunoBlock,omitempty" toml:",omitempty"`      // brunoBlock switch block (nil = no fork, 0 = already activated)
	EulerBlock      *big.Int `json:"eulerBlock,omitempty" toml:",omitempty"`      // eulerBlock switch block (nil = no fork, 0 = already activated)
	GibbsBlock      *big.Int `json:"gibbsBlock,omitempty" toml:",omitempty"`      // gibbsBlock switch block (nil = no fork, 0 = already activated)
	NanoBlock       *big.Int `json:"nanoBlock,omitempty" toml:",omitempty"`       // nanoBlock switch block (nil = no fork, 0 = already activated)
	MoranBlock      *big.Int `json:"moranBlock,omitempty" toml:",omitempty"`      // moranBlock switch block (nil = no fork, 0 = already activated)

	// Various consensus engines
	Ethash *EthashConfig `json:"ethash,omitempty" toml:",omitempty"`
	Clique *CliqueConfig `json:"clique,omitempty" toml:",omitempty"`
	Parlia *ParliaConfig `json:"parlia,omitempty" toml:",omitempty"`
	Anchor *AnchorConfig `json:"anchor,omitempty" toml:",omitempty"`

	GasPrice *big.Int `json:"GasPrice,omitempty"`
}

// EthashConfig is the consensus engine configs for proof-of-work based sealing.
type EthashConfig struct{}

// String implements the stringer interface, returning the consensus engine details.
func (c *EthashConfig) String() string {
	return "ethash"
}

// CliqueConfig is the consensus engine configs for proof-of-authority based sealing.
type CliqueConfig struct {
	Period uint64 `json:"period"` // Number of seconds between blocks to enforce
	Epoch  uint64 `json:"epoch"`  // Epoch length to reset votes and checkpoint
}

// String implements the stringer interface, returning the consensus engine details.
func (c *CliqueConfig) String() string {
	return "clique"
}

// ParliaConfig is the consensus engine configs for proof-of-staked-authority based sealing.
type ParliaConfig struct {
	Period           uint64         `json:"period"` // Number of seconds between blocks to enforce
	Epoch            uint64         `json:"epoch"`  // Epoch length to update validatorSet
	BlockFeeRate     uint64         `json:"blockFeeRate"`
	BlockFeeReceiver common.Address `json:"blockFeeReceiver"`
}

// String implements the stringer interface, returning the consensus engine details.
func (b *ParliaConfig) String() string {
	return "parlia"
}

type AnchorConfig struct {
	Period                    uint64         `json:"period"` // Number of seconds between blocks to enforce
	Epoch                     uint64         `json:"epoch"`  // Epoch length to update validatorSet
	ForkBlockNumber           uint64         `json:"forkBlockNumber"`
	IPCPath                   string         `json:"ipcPath"`
	WaitingConfirmBlockNumber uint64         `json:"anchorConfirmBlockNumber"`
	GenesisAddress            common.Address `json:"genesisAddress"`
	ManagerAddress            common.Address `json:"genesisAddress"`
	CacheDataBase             ethdb.Database
}

func (b *AnchorConfig) String() string {
	return "anchor"
}

func (b *AnchorConfig) ConfirmVersionBlockNumber(blockNumber uint64) uint64 {
	return b.ForkBlockNumber + blockNumber + b.WaitingConfirmBlockNumber
}

func (b *AnchorConfig) AnchorBlockNumber(blockNumber uint64) uint64 {
	return b.ForkBlockNumber + blockNumber
}

// String implements the fmt.Stringer interface.
func (c *ChainConfig) String() string {
	var engine interface{}
	switch {
	case c.Ethash != nil:
		engine = c.Ethash
	case c.Clique != nil:
		engine = c.Clique
	case c.Parlia != nil:
		engine = c.Parlia
	case c.Anchor != nil:
		engine = c.Anchor
	default:
		engine = "unknown"
	}
	return fmt.Sprintf("{ChainID: %v Homestead: %v DAO: %v DAOSupport: %v EIP150: %v EIP155: %v EIP158: %v Byzantium: %v Constantinople: %v Petersburg: %v Istanbul: %v, Muir Glacier: %v, Ramanujan: %v, Niels: %v, MirrorSync: %v, Bruno: %v, Berlin: %v, YOLO v3: %v, CatalystBlock: %v, London: %v, ArrowGlacier: %v, MergeFork:%v, Euler: %v, Gibbs: %v, Nano: %v, Moran: %v, Engine: %v}",
		c.ChainID,
		c.HomesteadBlock,
		c.DAOForkBlock,
		c.DAOForkSupport,
		c.EIP150Block,
		c.EIP155Block,
		c.EIP158Block,
		c.ByzantiumBlock,
		c.ConstantinopleBlock,
		c.PetersburgBlock,
		c.IstanbulBlock,
		c.MuirGlacierBlock,
		c.RamanujanBlock,
		c.NielsBlock,
		c.MirrorSyncBlock,
		c.BrunoBlock,
		c.BerlinBlock,
		c.YoloV3Block,
		c.CatalystBlock,
		c.LondonBlock,
		c.ArrowGlacierBlock,
		c.MergeForkBlock,
		c.EulerBlock,
		c.GibbsBlock,
		c.NanoBlock,
		c.MoranBlock,
		engine,
	)
}

// IsHomestead returns whether num is either equal to the homestead block or greater.
func (c *ChainConfig) IsHomestead(num *big.Int) bool {
	return isForked(c.HomesteadBlock, num)
}

// IsDAOFork returns whether num is either equal to the DAO fork block or greater.
func (c *ChainConfig) IsDAOFork(num *big.Int) bool {
	return isForked(c.DAOForkBlock, num)
}

// IsEIP150 returns whether num is either equal to the EIP150 fork block or greater.
func (c *ChainConfig) IsEIP150(num *big.Int) bool {
	return isForked(c.EIP150Block, num)
}

// IsEIP155 returns whether num is either equal to the EIP155 fork block or greater.
func (c *ChainConfig) IsEIP155(num *big.Int) bool {
	return isForked(c.EIP155Block, num)
}

// IsEIP158 returns whether num is either equal to the EIP158 fork block or greater.
func (c *ChainConfig) IsEIP158(num *big.Int) bool {
	return isForked(c.EIP158Block, num)
}

// IsByzantium returns whether num is either equal to the Byzantium fork block or greater.
func (c *ChainConfig) IsByzantium(num *big.Int) bool {
	return isForked(c.ByzantiumBlock, num)
}

// IsConstantinople returns whether num is either equal to the Constantinople fork block or greater.
func (c *ChainConfig) IsConstantinople(num *big.Int) bool {
	return isForked(c.ConstantinopleBlock, num)
}

// IsRamanujan returns whether num is either equal to the IsRamanujan fork block or greater.
func (c *ChainConfig) IsRamanujan(num *big.Int) bool {
	return isForked(c.RamanujanBlock, num)
}

// IsOnRamanujan returns whether num is equal to the Ramanujan fork block
func (c *ChainConfig) IsOnRamanujan(num *big.Int) bool {
	return configNumEqual(c.RamanujanBlock, num)
}

// IsNiels returns whether num is either equal to the Niels fork block or greater.
func (c *ChainConfig) IsNiels(num *big.Int) bool {
	return isForked(c.NielsBlock, num)
}

// IsOnNiels returns whether num is equal to the IsNiels fork block
func (c *ChainConfig) IsOnNiels(num *big.Int) bool {
	return configNumEqual(c.NielsBlock, num)
}

// IsMirrorSync returns whether num is either equal to the MirrorSync fork block or greater.
func (c *ChainConfig) IsMirrorSync(num *big.Int) bool {
	return isForked(c.MirrorSyncBlock, num)
}

// IsOnMirrorSync returns whether num is equal to the MirrorSync fork block
func (c *ChainConfig) IsOnMirrorSync(num *big.Int) bool {
	return configNumEqual(c.MirrorSyncBlock, num)
}

// IsBruno returns whether num is either equal to the Burn fork block or greater.
func (c *ChainConfig) IsBruno(num *big.Int) bool {
	return isForked(c.BrunoBlock, num)
}

// IsOnBruno returns whether num is equal to the Burn fork block
func (c *ChainConfig) IsOnBruno(num *big.Int) bool {
	return configNumEqual(c.BrunoBlock, num)
}

// IsEuler returns whether num is either equal to the euler fork block or greater.
func (c *ChainConfig) IsEuler(num *big.Int) bool {
	return isForked(c.EulerBlock, num)
}

// IsOnEuler returns whether num is equal to the euler fork block
func (c *ChainConfig) IsOnEuler(num *big.Int) bool {
	return configNumEqual(c.EulerBlock, num)
}

// IsMuirGlacier returns whether num is either equal to the Muir Glacier (EIP-2384) fork block or greater.
func (c *ChainConfig) IsMuirGlacier(num *big.Int) bool {
	return isForked(c.MuirGlacierBlock, num)
}

// IsPetersburg returns whether num is either
// - equal to or greater than the PetersburgBlock fork block,
// - OR is nil, and Constantinople is active
func (c *ChainConfig) IsPetersburg(num *big.Int) bool {
	return isForked(c.PetersburgBlock, num) || c.PetersburgBlock == nil && isForked(c.ConstantinopleBlock, num)
}

// IsIstanbul returns whether num is either equal to the Istanbul fork block or greater.
func (c *ChainConfig) IsIstanbul(num *big.Int) bool {
	return isForked(c.IstanbulBlock, num)
}

// IsBerlin returns whether num is either equal to the Berlin fork block or greater.
func (c *ChainConfig) IsBerlin(num *big.Int) bool {
	return isForked(c.BerlinBlock, num)
}

// IsLondon returns whether num is either equal to the London fork block or greater.
func (c *ChainConfig) IsLondon(num *big.Int) bool {
	return isForked(c.LondonBlock, num)
}

// IsArrowGlacier returns whether num is either equal to the Arrow Glacier (EIP-4345) fork block or greater.
func (c *ChainConfig) IsArrowGlacier(num *big.Int) bool {
	return isForked(c.ArrowGlacierBlock, num)
}

// IsTerminalPoWBlock returns whether the given block is the last block of PoW stage.
func (c *ChainConfig) IsTerminalPoWBlock(parentTotalDiff *big.Int, totalDiff *big.Int) bool {
	if c.TerminalTotalDifficulty == nil {
		return false
	}
	return parentTotalDiff.Cmp(c.TerminalTotalDifficulty) < 0 && totalDiff.Cmp(c.TerminalTotalDifficulty) >= 0
}

// IsGibbs returns whether num is either equal to the gibbs fork block or greater.
func (c *ChainConfig) IsGibbs(num *big.Int) bool {
	return isForked(c.GibbsBlock, num)
}

// IsOnGibbs returns whether num is equal to the gibbs fork block
func (c *ChainConfig) IsOnGibbs(num *big.Int) bool {
	return configNumEqual(c.GibbsBlock, num)
}

func (c *ChainConfig) IsNano(num *big.Int) bool {
	return isForked(c.NanoBlock, num)
}

func (c *ChainConfig) IsOnNano(num *big.Int) bool {
	return configNumEqual(c.NanoBlock, num)
}

func (c *ChainConfig) IsMoran(num *big.Int) bool {
	return isForked(c.MoranBlock, num)
}

func (c *ChainConfig) IsOnMoran(num *big.Int) bool {
	return configNumEqual(c.MoranBlock, num)
}

// CheckCompatible checks whether scheduled fork transitions have been imported
// with a mismatching chain configuration.
func (c *ChainConfig) CheckCompatible(newcfg *ChainConfig, height uint64) *ConfigCompatError {
	bhead := new(big.Int).SetUint64(height)

	// Iterate checkCompatible to find the lowest conflict.
	var lasterr *ConfigCompatError
	for {
		err := c.checkCompatible(newcfg, bhead)
		if err == nil || (lasterr != nil && err.RewindTo == lasterr.RewindTo) {
			break
		}
		lasterr = err
		bhead.SetUint64(err.RewindTo)
	}
	return lasterr
}

// CheckConfigForkOrder checks that we don't "skip" any forks, geth isn't pluggable enough
// to guarantee that forks can be implemented in a different order than on official networks
func (c *ChainConfig) CheckConfigForkOrder() error {
	type fork struct {
		name     string
		block    *big.Int
		optional bool // if true, the fork may be nil and next fork is still allowed
	}
	var lastFork fork
	for _, cur := range []fork{
		{name: "mirrorSyncBlock", block: c.MirrorSyncBlock},
		{name: "brunoBlock", block: c.BrunoBlock},
		{name: "eulerBlock", block: c.EulerBlock},
		{name: "gibbsBlock", block: c.GibbsBlock},
	} {
		if lastFork.name != "" {
			// Next one must be higher number
			if lastFork.block == nil && cur.block != nil {
				return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at %v",
					lastFork.name, cur.name, cur.block)
			}
			if lastFork.block != nil && cur.block != nil {
				if lastFork.block.Cmp(cur.block) > 0 {
					return fmt.Errorf("unsupported fork ordering: %v enabled at %v, but %v enabled at %v",
						lastFork.name, lastFork.block, cur.name, cur.block)
				}
			}
		}
		// If it was optional and not set, then ignore it
		if !cur.optional || cur.block != nil {
			lastFork = cur
		}
	}
	return nil
}

func (c *ChainConfig) checkCompatible(newcfg *ChainConfig, head *big.Int) *ConfigCompatError {
	if isForkIncompatible(c.HomesteadBlock, newcfg.HomesteadBlock, head) {
		return newCompatError("Homestead fork block", c.HomesteadBlock, newcfg.HomesteadBlock)
	}
	if isForkIncompatible(c.DAOForkBlock, newcfg.DAOForkBlock, head) {
		return newCompatError("DAO fork block", c.DAOForkBlock, newcfg.DAOForkBlock)
	}
	if c.IsDAOFork(head) && c.DAOForkSupport != newcfg.DAOForkSupport {
		return newCompatError("DAO fork support flag", c.DAOForkBlock, newcfg.DAOForkBlock)
	}
	if isForkIncompatible(c.EIP150Block, newcfg.EIP150Block, head) {
		return newCompatError("EIP150 fork block", c.EIP150Block, newcfg.EIP150Block)
	}
	if isForkIncompatible(c.EIP155Block, newcfg.EIP155Block, head) {
		return newCompatError("EIP155 fork block", c.EIP155Block, newcfg.EIP155Block)
	}
	if isForkIncompatible(c.EIP158Block, newcfg.EIP158Block, head) {
		return newCompatError("EIP158 fork block", c.EIP158Block, newcfg.EIP158Block)
	}
	if c.IsEIP158(head) && !configNumEqual(c.ChainID, newcfg.ChainID) {
		return newCompatError("EIP158 chain ID", c.EIP158Block, newcfg.EIP158Block)
	}
	if isForkIncompatible(c.ByzantiumBlock, newcfg.ByzantiumBlock, head) {
		return newCompatError("Byzantium fork block", c.ByzantiumBlock, newcfg.ByzantiumBlock)
	}
	if isForkIncompatible(c.ConstantinopleBlock, newcfg.ConstantinopleBlock, head) {
		return newCompatError("Constantinople fork block", c.ConstantinopleBlock, newcfg.ConstantinopleBlock)
	}
	if isForkIncompatible(c.PetersburgBlock, newcfg.PetersburgBlock, head) {
		// the only case where we allow Petersburg to be set in the past is if it is equal to Constantinople
		// mainly to satisfy fork ordering requirements which state that Petersburg fork be set if Constantinople fork is set
		if isForkIncompatible(c.ConstantinopleBlock, newcfg.PetersburgBlock, head) {
			return newCompatError("Petersburg fork block", c.PetersburgBlock, newcfg.PetersburgBlock)
		}
	}
	if isForkIncompatible(c.IstanbulBlock, newcfg.IstanbulBlock, head) {
		return newCompatError("Istanbul fork block", c.IstanbulBlock, newcfg.IstanbulBlock)
	}
	if isForkIncompatible(c.MuirGlacierBlock, newcfg.MuirGlacierBlock, head) {
		return newCompatError("Muir Glacier fork block", c.MuirGlacierBlock, newcfg.MuirGlacierBlock)
	}
	if isForkIncompatible(c.BerlinBlock, newcfg.BerlinBlock, head) {
		return newCompatError("Berlin fork block", c.BerlinBlock, newcfg.BerlinBlock)
	}
	if isForkIncompatible(c.LondonBlock, newcfg.LondonBlock, head) {
		return newCompatError("London fork block", c.LondonBlock, newcfg.LondonBlock)
	}
	if isForkIncompatible(c.ArrowGlacierBlock, newcfg.ArrowGlacierBlock, head) {
		return newCompatError("Arrow Glacier fork block", c.ArrowGlacierBlock, newcfg.ArrowGlacierBlock)
	}
	if isForkIncompatible(c.MergeForkBlock, newcfg.MergeForkBlock, head) {
		return newCompatError("Merge Start fork block", c.MergeForkBlock, newcfg.MergeForkBlock)
	}
	if isForkIncompatible(c.RamanujanBlock, newcfg.RamanujanBlock, head) {
		return newCompatError("ramanujan fork block", c.RamanujanBlock, newcfg.RamanujanBlock)
	}
	if isForkIncompatible(c.MirrorSyncBlock, newcfg.MirrorSyncBlock, head) {
		return newCompatError("mirrorSync fork block", c.MirrorSyncBlock, newcfg.MirrorSyncBlock)
	}
	if isForkIncompatible(c.BrunoBlock, newcfg.BrunoBlock, head) {
		return newCompatError("bruno fork block", c.BrunoBlock, newcfg.BrunoBlock)
	}
	if isForkIncompatible(c.EulerBlock, newcfg.EulerBlock, head) {
		return newCompatError("euler fork block", c.EulerBlock, newcfg.EulerBlock)
	}
	if isForkIncompatible(c.GibbsBlock, newcfg.GibbsBlock, head) {
		return newCompatError("gibbs fork block", c.GibbsBlock, newcfg.GibbsBlock)
	}
	if isForkIncompatible(c.NanoBlock, newcfg.NanoBlock, head) {
		return newCompatError("nano fork block", c.NanoBlock, newcfg.NanoBlock)
	}
	if isForkIncompatible(c.MoranBlock, newcfg.MoranBlock, head) {
		return newCompatError("moran fork block", c.MoranBlock, newcfg.MoranBlock)
	}
	return nil
}

// isForkIncompatible returns true if a fork scheduled at s1 cannot be rescheduled to
// block s2 because head is already past the fork.
func isForkIncompatible(s1, s2, head *big.Int) bool {
	return (isForked(s1, head) || isForked(s2, head)) && !configNumEqual(s1, s2)
}

// isForked returns whether a fork scheduled at block s is active at the given head block.
func isForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}

func (c *ChainConfig) IsUnderCommunitySwap(num *big.Int) bool {
	return isForked(c.GetCommunitySwapBlock(), num)
}

func (c *ChainConfig) IsOnValidatorDeployForkBlock(num *big.Int) bool {
	return configNumEqual(c.GetValidatorBlock(), num)
}

func (c *ChainConfig) IsOnValidatorActivityForkBlock(num *big.Int) bool {
	return configNumEqual(c.GetCommunitySwapBlock(), num)
}

func (c *ChainConfig) GetCommunitySwapBlock() *big.Int {
	switch c.ChainID.String() {
	case "1337":
		return big.NewInt(CommunitySwapBlockDev)
	case "20241104":
		return big.NewInt(CommunitySwapBlockTest)
	case "140705":
		return big.NewInt(CommunitySwapBlockMain)
	default:
		return big.NewInt(CommunitySwapBlockMain)
	}
}

func (c *ChainConfig) GetValidatorBlock() *big.Int {
	switch c.ChainID.String() {
	case "1337":
		return big.NewInt(DeployValidatorBlockDev)
	case "20241104":
		return big.NewInt(DeployValidatorBlockTest)
	case "140705":
		return big.NewInt(DeployValidatorBlock)
	default:
		return big.NewInt(DeployValidatorBlock)
	}
}

func configNumEqual(x, y *big.Int) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return x.Cmp(y) == 0
}

// ConfigCompatError is raised if the locally-stored blockchain is initialised with a
// ChainConfig that would alter the past.
type ConfigCompatError struct {
	What string
	// block numbers of the stored and new configurations
	StoredConfig, NewConfig *big.Int
	// the block number to which the local chain must be rewound to correct the error
	RewindTo uint64
}

func newCompatError(what string, storedblock, newblock *big.Int) *ConfigCompatError {
	var rew *big.Int
	switch {
	case storedblock == nil:
		rew = newblock
	case newblock == nil || storedblock.Cmp(newblock) < 0:
		rew = storedblock
	default:
		rew = newblock
	}
	err := &ConfigCompatError{what, storedblock, newblock, 0}
	if rew != nil && rew.Sign() > 0 {
		err.RewindTo = rew.Uint64() - 1
	}
	return err
}

func (err *ConfigCompatError) Error() string {
	return fmt.Sprintf("mismatching %s in database (have %d, want %d, rewindto %d)", err.What, err.StoredConfig, err.NewConfig, err.RewindTo)
}

// Rules wraps ChainConfig and is merely syntactic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules struct {
	ChainID                                                 *big.Int
	IsHomestead, IsEIP150, IsEIP155, IsEIP158               bool
	IsByzantium, IsConstantinople, IsPetersburg, IsIstanbul bool
	IsBerlin, IsLondon                                      bool
	IsMerge                                                 bool
	IsNano                                                  bool
	IsMoran                                                 bool
}

// Rules ensures c's ChainID is not nil.
func (c *ChainConfig) Rules(num *big.Int, isMerge bool) Rules {
	chainID := c.ChainID
	if chainID == nil {
		chainID = new(big.Int)
	}
	return Rules{
		ChainID:          new(big.Int).Set(chainID),
		IsHomestead:      c.IsHomestead(num),
		IsEIP150:         c.IsEIP150(num),
		IsEIP155:         c.IsEIP155(num),
		IsEIP158:         c.IsEIP158(num),
		IsByzantium:      c.IsByzantium(num),
		IsConstantinople: c.IsConstantinople(num),
		IsPetersburg:     c.IsPetersburg(num),
		IsIstanbul:       c.IsIstanbul(num),
		IsBerlin:         c.IsBerlin(num),
		IsLondon:         c.IsLondon(num),
		IsMerge:          isMerge,
		IsNano:           c.IsNano(num),
		IsMoran:          c.IsMoran(num),
	}
}
