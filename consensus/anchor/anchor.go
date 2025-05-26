package anchor

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/consensus/farms"
	"github.com/ethereum/go-ethereum/core/systemcontracts/anchor"
	"github.com/ethereum/go-ethereum/ethclient"
	"io"
	"math"
	"math/big"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	inMemorySnapshots  = 128  // Number of recent snapshots to keep in memory
	inMemorySignatures = 4096 // Number of recent block signatures to keep in memory

	checkpointInterval = 1024        // Number of blocks after which to save the snapshot to the database
	defaultEpochLength = uint64(100) // Default number of blocks of checkpoint to update validatorSet from contract

	extraVanity      = 32 // Fixed number of extra-data prefix bytes reserved for signer vanity
	extraAnchorHash  = 32
	extraSeal        = 65 // Fixed number of extra-data suffix bytes reserved for signer seal
	nextForkHashSize = 4  // Fixed number of extra-data suffix bytes reserved for nextForkHash.

	validatorBytesLength = common.AddressLength
	wiggleTime           = uint64(1) // second, Random delay (per signer) to allow concurrent signers
	initialBackOffTime   = uint64(1) // second
	processBackOffTime   = uint64(1) // second
)

var (
	uncleHash  = types.CalcUncleHash(nil) // Always Keccak256(RLP([])) as uncles are meaningless outside of PoW.
	diffInTurn = big.NewInt(2)            // Block difficulty for in-turn signatures
	diffNoTurn = big.NewInt(1)            // Block difficulty for out-of-turn signatures
	// 100 native token
	maxSystemBalance = new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether))

	systemContracts = map[common.Address]bool{
		common.HexToAddress(systemcontracts.AddressTreeContract): true,
		common.HexToAddress(systemcontracts.SystemDaoContract):   true,
		common.HexToAddress(systemcontracts.FarmContract):        true,
		common.HexToAddress(systemcontracts.AnchorContract):      true,
	}
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	// errUnknownBlock is returned when the list of validators is requested for a block
	// that is not part of the local blockchain.
	errUnknownBlock = errors.New("unknown block")

	// errMissingVanity is returned if a block's extra-data section is shorter than
	// 32 bytes, which is required to store the signer vanity.
	errMissingVanity = errors.New("extra-data 32 byte vanity prefix missing")

	errMissingAnchorHash = errors.New("extra-data 32 byte anchor block hash prefix missing")

	// errMissingSignature is returned if a block's extra-data section doesn't seem
	// to contain a 65 byte secp256k1 signature.
	errMissingSignature = errors.New("extra-data 65 byte signature suffix missing")

	// errExtraValidators is returned if non-sprint-end block contain validator data in
	// their extra-data fields.
	errExtraValidators = errors.New("non-sprint-end block contains extra validator list")

	// errInvalidSpanValidators is returned if a block contains an
	// invalid list of validators (i.e. non divisible by 20 bytes).
	errInvalidSpanValidators = errors.New("invalid validator list on sprint end block")

	// errInvalidMixDigest is returned if a block's mix digest is non-zero.
	errInvalidMixDigest = errors.New("non-zero mix digest")

	// errInvalidUncleHash is returned if a block contains an non-empty uncle list.
	errInvalidUncleHash = errors.New("non empty uncle hash")

	// errMismatchingEpochValidators is returned if a sprint block contains a
	// list of validators different than the one the local node calculated.
	errMismatchingEpochValidators = errors.New("mismatching validator list on epoch block")

	// errInvalidDifficulty is returned if the difficulty of a block is missing.
	errInvalidDifficulty = errors.New("invalid difficulty")

	// errWrongDifficulty is returned if the difficulty of a block doesn't match the
	// turn of the signer.
	errWrongDifficulty = errors.New("wrong difficulty")

	// errOutOfRangeChain is returned if an authorization list is attempted to
	// be modified via out-of-range or non-contiguous headers.
	errOutOfRangeChain = errors.New("out of range or non-contiguous chain")

	// errBlockHashInconsistent is returned if an authorization list is attempted to
	// insert an inconsistent block.
	errBlockHashInconsistent = errors.New("the block hash is inconsistent")

	// errUnauthorizedValidator is returned if a header is signed by a non-authorized entity.
	errUnauthorizedValidator = errors.New("unauthorized validator")

	// errCoinBaseMisMatch is returned if a header's coinbase do not match with signature
	errCoinBaseMisMatch = errors.New("coinbase do not match with signature")

	// errRecentlySigned is returned if a header is signed by an authorized entity
	// that already signed a header recently, thus is temporarily not allowed to.
	errRecentlySigned = errors.New("recently signed")

	errInterruptPrepare = errors.New("interrupt Signal Received During Wait")
)

// SignerFn is a signer callback function to request a header to be signed by a
// backing account.
type SignerFn func(accounts.Account, string, []byte) ([]byte, error)
type SignerTxFn func(accounts.Account, *types.Transaction, *big.Int) (*types.Transaction, error)
type SignTextFn func(account accounts.Account, text []byte) ([]byte, error)

// ecrecover extracts the Ethereum account address from a signed header.
func ecrecover(header *types.Header, sigCache *lru.ARCCache, chainId *big.Int) (common.Address, error) {
	// If the signature's already cached, return that
	hash := header.Hash()
	if address, known := sigCache.Get(hash); known {
		return address.(common.Address), nil
	}
	// Retrieve the signature from the header extra-data
	if len(header.Extra) < extraSeal {
		return common.Address{}, errMissingSignature
	}
	signature := header.Extra[len(header.Extra)-extraSeal:]

	// Recover the public key and the Ethereum address
	pubkey, err := crypto.Ecrecover(SealHash(header, chainId).Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])

	sigCache.Add(hash, signer)
	return signer, nil
}

// AnchorRLP returns the rlp bytes which needs to be signed for the parlia
// sealing. The RLP to sign consists of the entire header apart from the 65 byte signature
// contained at the end of the extra data.
//
// Note, the method requires the extra data to be at least 65 bytes, otherwise it
// panics. This is done to avoid accidentally using both forms (signature present
// or not), which could be abused to produce different hashes for the same header.
func AnchorRLP(header *types.Header, chainId *big.Int) []byte {
	b := new(bytes.Buffer)
	encodeSigHeader(b, header, chainId)
	return b.Bytes()
}

type Anchor struct {
	chainConfig *params.ChainConfig  // Chain config
	config      *params.AnchorConfig // Consensus engine configuration parameters for parlia consensus
	genesisHash common.Hash
	db          ethdb.Database // Database to store and retrieve snapshot checkpoints

	recentSnaps *lru.ARCCache // Snapshots for recent block to speed up
	signatures  *lru.ARCCache // Signatures of recent blocks to speed up mining

	signer types.Signer

	val        common.Address // Ethereum address of the signing key
	signFn     SignerFn       // Signer function to authorize hashes with
	signTxFn   SignerTxFn
	signTextFn SignTextFn

	lock sync.RWMutex // Protects the signer fields

	ethAPI       *ethapi.PublicBlockChainAPI
	systemDaoABI abi.ABI
	anchorABI    abi.ABI

	// The fields below are for testing only
	fakeDiff bool // Skip difficulty verifications

	mainIPC        *ethclient.Client
	anchorContract *L2AnchorContract
}

/**
sdb := state.NewDatabase(db)
	statedb, _ := state.New(common.Hash{}, sdb, nil)
	for addr, a := range accounts {
		statedb.SetCode(addr, a.Code)
		statedb.SetNonce(addr, a.Nonce)
		statedb.SetBalance(addr, a.Balance)
		for k, v := range a.Storage {
			statedb.SetState(addr, k, v)
		}
	}
	// Commit and re-open to start with a clean state.
	statedb.Finalise(false)
	statedb.AccountsIntermediateRoot()
	root, _, _ := statedb.Commit(nil)
	statedb, _ = state.New(root, sdb, nil)
	return statedb

*/

// New creates a Anchor consensus engine.
func New(
	chainConfig *params.ChainConfig,
	db ethdb.Database,
	ethAPI *ethapi.PublicBlockChainAPI,
	genesisHash common.Hash,
) *Anchor {

	client, err := ethclient.Dial(chainConfig.Anchor.IPCPath)
	if err != nil {
		panic(err)
	}
	// get parlia config
	anchorConfig := chainConfig.Anchor

	// Set any missing consensus parameters to their defaults
	if anchorConfig != nil && anchorConfig.Epoch == 0 {
		anchorConfig.Epoch = defaultEpochLength
	}

	anchorContract, err := NewAnchorContract(client, ethAPI, chainConfig)
	if err != nil {
		panic(err)
	}

	// Allocate the snapshot caches and create the engine
	recentSnaps, err := lru.NewARC(inMemorySnapshots)
	if err != nil {
		panic(err)
	}
	signatures, err := lru.NewARC(inMemorySignatures)
	if err != nil {
		panic(err)
	}

	daoABI, err := abi.JSON(strings.NewReader(anchor.SystemDaoABI))
	if err != nil {
		panic(err)
	}

	anchorABI, err := abi.JSON(strings.NewReader(anchor.AnchorABI))
	if err != nil {
		panic(err)
	}

	c := &Anchor{
		chainConfig:    chainConfig,
		config:         anchorConfig,
		genesisHash:    genesisHash,
		db:             db,
		ethAPI:         ethAPI,
		recentSnaps:    recentSnaps,
		signatures:     signatures,
		signer:         types.NewLondonSigner(chainConfig.ChainID), //.NewEIP155Signer(chainConfig.ChainID),
		systemDaoABI:   daoABI,
		anchorABI:      anchorABI,
		mainIPC:        client,
		anchorContract: anchorContract,
	}
	return c
}

func (p *Anchor) isSubmitRequestProofTransaction(tx *types.Transaction, header *types.Header) (bool, error) {
	// deploy a contract
	if tx.To() == nil {
		return false, nil
	}

	sender, err := types.Sender(p.signer, tx)
	if err != nil {
		return false, errors.New("UnAuthorized transaction")
	}

	submitFnSig, existed := p.anchorABI.Methods["submitRequestProof"]
	if !existed {
		panic("submitRequestProof methods not in abi files")
	}

	//common.Bytes2Hex(input[0:4]), "bec23523")
	if sender == header.Coinbase && *tx.To() == common.HexToAddress(systemcontracts.AnchorContract) && bytes.Equal(tx.Data()[0:4], submitFnSig.ID) && tx.GasPrice().Cmp(big.NewInt(0)) == 0 {
		return true, nil
	}
	return false, nil
}

func (p *Anchor) IsSystemTransaction(tx *types.Transaction, header *types.Header) (bool, error) {
	// deploy a contract
	if tx.To() == nil {
		return false, nil
	}

	sender, err := types.Sender(p.signer, tx)
	if err != nil {
		return false, errors.New("UnAuthorized transaction")
	}

	if sender == header.Coinbase && p.IsSystemContract(tx.To()) && tx.GasPrice().Cmp(big.NewInt(0)) == 0 {
		return true, nil
	}

	return false, nil
}

func (p *Anchor) IsSystemContract(to *common.Address) bool {
	if to == nil {
		return false
	}
	return systemContracts[*to]
}

// Author implements consensus.Engine, returning the SystemAddress
func (p *Anchor) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

// VerifyHeader checks whether a header conforms to the consensus rules.
func (p *Anchor) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
	return p.verifyHeader(chain, header, nil)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers. The
// method returns a quit channel to abort the operations and a results channel to
// retrieve the async verifications (the order is that of the input slice).
func (p *Anchor) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	gopool.Submit(func() {
		for i, header := range headers {
			err := p.verifyHeader(chain, header, headers[:i])

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	})
	return abort, results
}

// verifyHeader checks whether a header conforms to the consensus rules.The
// caller may optionally pass in a batch of parents (ascending order) to avoid
// looking those up from the database. This is useful for concurrently verifying
// a batch of new headers.
func (p *Anchor) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	if header.Number == nil {
		return errUnknownBlock
	}
	number := header.Number.Uint64()

	// Don't waste time checking blocks from the future
	if header.Time > uint64(time.Now().Unix()) {
		return consensus.ErrFutureBlock
	}
	// Check that the extra-data contains the vanity, validators and signature.
	if len(header.Extra) < extraVanity {
		return errMissingVanity
	}
	if len(header.Extra) < extraVanity+extraAnchorHash {
		return errMissingAnchorHash
	}
	if len(header.Extra) < extraVanity+extraAnchorHash+extraSeal {
		return errMissingSignature
	}
	// check extra data
	isEpoch := number%p.config.Epoch == 0

	// Ensure that the extra-data contains a signer list on checkpoint, but none otherwise
	signersBytes := len(header.Extra) - extraVanity - extraAnchorHash - extraSeal
	if !isEpoch && signersBytes != 0 {
		return errExtraValidators
	}

	if isEpoch && signersBytes%validatorBytesLength != 0 {
		return errInvalidSpanValidators
	}

	// Ensure that the mix digest is zero as we don't have fork protection currently
	if header.MixDigest != (common.Hash{}) {
		return errInvalidMixDigest
	}
	// Ensure that the block doesn't contain any uncles which are meaningless in PoA
	if header.UncleHash != uncleHash {
		return errInvalidUncleHash
	}
	// Ensure that the block's difficulty is meaningful (may not be correct at this point)
	if number > 0 {
		if header.Difficulty == nil {
			return errInvalidDifficulty
		}
	}
	// If all checks passed, validate any special fields for hard forks
	if err := misc.VerifyForkHashes(chain.Config(), header, false); err != nil {
		return err
	}
	// All basic checks passed, verify cascading fields
	return p.verifyCascadingFields(chain, header, parents)
}

// verifyCascadingFields verifies all the header fields that are not standalone,
// rather depend on a batch of previous headers. The caller may optionally pass
// in a batch of parents (ascending order) to avoid looking those up from the
// database. This is useful for concurrently verifying a batch of new headers.
func (p *Anchor) verifyCascadingFields(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	// The genesis block is the always valid dead-end
	number := header.Number.Uint64()
	if number == 0 {
		return nil
	}

	var parent *types.Header
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(header.ParentHash, number-1)
	}

	if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
		return consensus.ErrUnknownAncestor
	}

	snap, err := p.snapshot(chain, number-1, header.ParentHash, parents)
	if err != nil {
		return err
	}

	err = p.blockTimeVerifyForRamanujanFork(snap, header, parent)
	if err != nil {
		return err
	}

	// Verify that the gas limit is <= 2^63-1
	capacity := uint64(0x7fffffffffffffff)
	if header.GasLimit > capacity {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, capacity)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}

	// Verify that the gas limit remains within allowed bounds
	diff := int64(parent.GasLimit) - int64(header.GasLimit)
	if diff < 0 {
		diff *= -1
	}
	limit := parent.GasLimit / params.GasLimitBoundDivisor

	if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
		return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
	}

	// All basic checks passed, verify the seal and return
	return p.verifySeal(chain, header, parents)
}

// snapshot retrieves the authorization snapshot at a given point in time.
func (p *Anchor) snapshot(chain consensus.ChainHeaderReader, number uint64, hash common.Hash, parents []*types.Header) (*Snapshot, error) {
	// Search for a snapshot in memory or on disk for checkpoints
	var (
		headers []*types.Header
		snap    *Snapshot
	)

	for snap == nil {
		// If an in-memory snapshot was found, use that
		if s, ok := p.recentSnaps.Get(hash); ok {
			snap = s.(*Snapshot)
			break
		}

		// If an on-disk checkpoint snapshot can be found, use that
		if number%checkpointInterval == 0 {
			if s, err := loadSnapshot(p.config, p.signatures, p.db, hash, p.ethAPI); err == nil {
				log.Trace("Loaded snapshot from disk", "number", number, "hash", hash)
				snap = s
				break
			}
		}

		// If we're at the genesis, snapshot the initial state.
		if number == 0 {
			checkpoint := chain.GetHeaderByNumber(number)
			if checkpoint != nil {
				// get checkpoint data
				hash := checkpoint.Hash()

				validatorBytes := checkpoint.Extra[extraVanity : len(checkpoint.Extra)-extraSeal-extraAnchorHash]
				// get validators from headers
				validators, err := ParseValidators(validatorBytes)
				if err != nil {
					return nil, err
				}

				// new snap shot
				snap = newSnapshot(p.config, p.signatures, number, hash, validators, p.ethAPI)
				if err := snap.store(p.db); err != nil {
					return nil, err
				}
				log.Info("Stored checkpoint snapshot to disk", "number", number, "hash", hash)
				break
			}
		}

		// No snapshot for this header, gather the header and move backward
		var header *types.Header
		if len(parents) > 0 {
			// If we have explicit parents, pick from there (enforced)
			header = parents[len(parents)-1]
			if header.Hash() != hash || header.Number.Uint64() != number {
				return nil, consensus.ErrUnknownAncestor
			}
			parents = parents[:len(parents)-1]
		} else {
			// No explicit parents (or no more left), reach out to the database
			header = chain.GetHeader(hash, number)
			if header == nil {
				return nil, consensus.ErrUnknownAncestor
			}
		}
		headers = append(headers, header)
		number, hash = number-1, header.ParentHash
	}

	// check if snapshot is nil
	if snap == nil {
		return nil, fmt.Errorf("unknown error while retrieving snapshot at block number %v", number)
	}

	// Previous snapshot found, apply any pending headers on top of it
	for i := 0; i < len(headers)/2; i++ {
		headers[i], headers[len(headers)-1-i] = headers[len(headers)-1-i], headers[i]
	}

	snap, err := snap.apply(headers, chain, parents, p.chainConfig.ChainID, p.chainConfig.GetCommunitySwapBlock())
	if err != nil {
		return nil, err
	}
	p.recentSnaps.Add(snap.Hash, snap)

	// If we've generated a new checkpoint snapshot, save to disk
	if snap.Number%checkpointInterval == 0 && len(headers) > 0 {
		if err = snap.store(p.db); err != nil {
			return nil, err
		}
		log.Trace("Stored snapshot to disk", "number", snap.Number, "hash", snap.Hash)
	}
	return snap, err
}

// VerifyUncles implements consensus.Engine, always returning an error for any
// uncles as this consensus mechanism doesn't permit uncles.
func (p *Anchor) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errors.New("uncles not allowed")
	}
	return nil
}

// VerifySeal implements consensus.Engine, checking whether the signature contained
// in the header satisfies the consensus protocol requirements.
func (p *Anchor) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	return p.verifySeal(chain, header, nil)
}

// verifySeal checks whether the signature contained in the header satisfies the
// consensus protocol requirements. The method accepts an optional list of parent
// headers that aren't yet part of the local blockchain to generate the snapshots
// from.
func (p *Anchor) verifySeal(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	// Verifying the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	// Retrieve the snapshot needed to verify this header and cache it
	snap, err := p.snapshot(chain, number-1, header.ParentHash, parents)
	if err != nil {
		return err
	}

	// Resolve the authorization key and check against validators
	signer, err := ecrecover(header, p.signatures, p.chainConfig.ChainID)
	if err != nil {
		return err
	}

	if signer != header.Coinbase {
		return errCoinBaseMisMatch
	}

	if _, ok := snap.Validators[signer]; !ok {
		return errUnauthorizedValidator
	}

	for seen, recent := range snap.Recents {
		if recent == signer {
			// Signer is among recents, only fail if the current block doesn't shift it out
			if limit := uint64(len(snap.Validators)/2 + 1); seen > number-limit {
				return errRecentlySigned
			}
		}
	}

	// Ensure that the difficulty corresponds to the turn-ness of the signer
	if !p.fakeDiff {
		inturn := snap.inturn(signer)
		if inturn && header.Difficulty.Cmp(diffInTurn) != 0 {
			return errWrongDifficulty
		}
		if !inturn && header.Difficulty.Cmp(diffNoTurn) != 0 {
			return errWrongDifficulty
		}
	}

	// todo self block reward verify

	return nil
}

// Prepare implements consensus.Engine, preparing all the consensus fields of the
// header for running the transactions on top.
func (p *Anchor) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {

	header.Coinbase = p.val
	header.Nonce = types.BlockNonce{}
	number := header.Number.Uint64()
	anchorBlockNumber := p.config.AnchorBlockNumber(number)
	waitL1BlockNumber := p.config.ConfirmVersionBlockNumber(number)

	loopInterval := 2 * time.Second
	timer := time.NewTimer(0)
	logCount := 0
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM) // catch SIGINT and SIGTERM

WaitingLoop:
	anchorBlockHeight, err := p.mainIPC.BlockNumber(context.TODO())
	if err != nil {
		return err
	}

	if anchorBlockHeight < waitL1BlockNumber {
		if logCount%5 == 0 {
			log.Info("Anchor block not yet finalized", "number", number, "anchornumber", anchorBlockNumber, "waitingnumber", waitL1BlockNumber, "anchorheight", anchorBlockHeight, "sleep", 2)
		}
		logCount++

		for {
			select {
			case <-sigChan:
				return errInterruptPrepare

			case <-timer.C:
				timer.Reset(loopInterval)
				goto WaitingLoop
			}
		}
	}
	timer.Stop()

	anchorBlock, err := p.mainIPC.BlockByNumber(context.TODO(), big.NewInt(0).SetUint64(anchorBlockNumber))
	if err != nil {
		return err
	}

	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	// Set the correct difficulty
	header.Difficulty = CalcDifficulty(snap, p.val)

	// Ensure the extra data has all it's components
	if len(header.Extra) < extraVanity-nextForkHashSize {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, extraVanity-nextForkHashSize-len(header.Extra))...)
	}
	header.Extra = header.Extra[:extraVanity-nextForkHashSize]
	nextForkHash := forkid.NextForkHash(p.chainConfig, p.genesisHash, number)
	header.Extra = append(header.Extra, nextForkHash[:]...)

	// add extra anchor block hash
	header.Extra = append(header.Extra, anchorBlock.Hash().Bytes()...)

	// add extra seal space
	header.Extra = append(header.Extra, make([]byte, extraSeal)...)

	// Set gas price
	baseGasPrice, err := p.getBaseGasPrice(header.ParentHash)
	if err != nil {
		return err
	}
	chain.Config().GasPrice = baseGasPrice
	header.BaseFee = baseGasPrice

	// Mix digest is reserved for now, set to empty
	header.MixDigest = common.Hash{}

	// Ensure the timestamp has the correct delay
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Time = p.blockTimeForRamanujanFork(snap, header, parent)
	if header.Time < uint64(time.Now().Unix()) {
		header.Time = uint64(time.Now().Unix())
	}
	return nil
}

// Finalize implements consensus.Engine, ensuring no uncles are set, nor block
// rewards given.
func (p *Anchor) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs *[]*types.Transaction,
	uncles []*types.Header, receipts *[]*types.Receipt, systemTxs *[]*types.Transaction, usedGas *uint64) error {
	// warn if not in majority fork
	number := header.Number.Uint64()

	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}
	nextForkHash := forkid.NextForkHash(p.chainConfig, p.genesisHash, number)
	if !snap.isMajorityFork(hex.EncodeToString(nextForkHash[:])) {
		log.Debug("there is a possible fork, and your client is not the majority. Please check...", "nextForkHash", hex.EncodeToString(nextForkHash[:]))
	}

	// No block rewards in PoA, so the state remains as is and uncles are dropped
	cx := NewChainContext(chain, p)
	if header.Number.Cmp(common.Big1) == 0 {
		err := p.initContract(state, header, cx, txs, receipts, systemTxs, usedGas, false)
		if err != nil {
			log.Error("init contract failed")
		}
	}

	if header.Difficulty.Cmp(diffInTurn) != 0 {
		spoiledVal := snap.supposeValidator()
		signedRecently := false
		for _, recent := range snap.Recents {
			if recent == spoiledVal {
				signedRecently = true
				break
			}
		}
		if !signedRecently {
			log.Trace("slash validator", "block hash", header.Hash(), "address", spoiledVal)
			err = p.slash(spoiledVal, state, header, cx, txs, receipts, systemTxs, usedGas, false)
			if err != nil {
				// it is possible that slash validator failed because of the slash channel is disabled.
				log.Error("slash validator failed", "block hash", header.Hash(), "address", spoiledVal)
			}
		}
	}

	for _, sysTx := range *systemTxs {
		isSubmitProofTx, err := p.isSubmitRequestProofTransaction(sysTx, header)
		if err != nil {
			return err
		}

		if isSubmitProofTx {
			msg := p.getSystemMessage(
				header.Coinbase,
				common.HexToAddress(systemcontracts.AnchorContract),
				sysTx.Data(),
				common.Big0,
			)

			if err = p.applyTransaction(msg, state, header, cx, txs, receipts, systemTxs, usedGas, false); err != nil {
				return errors.New("submit burn proofs failed")
			}
		}
	}

	if err := p.handleBlockSubscriber(state, header, cx, txs, receipts, systemTxs, usedGas, false); err != nil {
		log.Warn("Farm HandleBlockSubscriber Error", err.Error())
	}

	// hande token exchange
	exTxs, err := p.anchorContract.l1ExchangesOfBlockNumber(p.config.AnchorBlockNumber(header.Number.Uint64()))
	if err != nil {
		return errors.New("get anchor net exchange transaction failed")
	}
	if err = p.handleAnchorTokenExchange(exTxs, state, header, cx, txs, receipts, systemTxs, usedGas, false); err != nil {
		return errors.New("handleAnchorTokenExchange transaction failed")
	}

	snapID := state.Snapshot()
	// handle farm
	farm := farms.NewWithAnchorNet(
		state,
		p.ethAPI,
		common.HexToAddress(systemcontracts.FarmContract),
		common.HexToAddress(systemcontracts.AddressTreeContract),
		p.mainIPC,
		p.config.AnchorBlockNumber(header.Number.Uint64()),
		&p.chainConfig.Anchor.CacheDataBase,
	)

	if err := farm.FinalizeBlock(cx, p.chainConfig, header, txs, receipts, true); err != nil {
		state.RevertToSnapshot(snapID)
		return err
	}

	if len(*systemTxs) > 0 {
		return errors.New("the length of systemTxs do not match")
	}

	return nil
}

// FinalizeAndAssemble implements consensus.Engine, ensuring no uncles are set,
// nor block rewards given, and returns the final block.
func (p *Anchor) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB,
	txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, []*types.Receipt, error) {
	// No block rewards in PoA, so the state remains as is and uncles are dropped

	cx := NewChainContext(chain, p)
	if txs == nil {
		txs = make([]*types.Transaction, 0)
	}
	if receipts == nil {
		receipts = make([]*types.Receipt, 0)
	}
	if header.Number.Cmp(common.Big1) == 0 {
		err := p.initContract(state, header, cx, &txs, &receipts, nil, &header.GasUsed, true)
		if err != nil {
			log.Error("init contract failed")
		}
	}

	if header.Difficulty.Cmp(diffInTurn) != 0 {
		number := header.Number.Uint64()
		snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
		if err != nil {
			return nil, nil, err
		}
		spoiledVal := snap.supposeValidator()
		signedRecently := false
		for _, recent := range snap.Recents {
			if recent == spoiledVal {
				signedRecently = true
				break
			}
		}
		if !signedRecently {
			err = p.slash(spoiledVal, state, header, cx, &txs, &receipts, nil, &header.GasUsed, true)
			if err != nil {
				// it is possible that slash validator failed because of the slash channel is disabled.
				log.Error("slash validator failed", "block hash", header.Hash(), "address", spoiledVal)
			}
		}
	}

	proofs, err := p.anchorContract.l2BurnProofs(header.ParentHash, header.Coinbase, p.signTextFn)
	if err != nil {
		log.Warn("Submit BurnProof Failed", "number", header.Number, "error", err.Error())
	} else if proofs != nil {
		for _, proof := range *proofs {
			data, err := p.anchorABI.Pack("submitRequestProof", proof.Index, proof.Signature)
			if err != nil {
				panic(err)
			}

			msg := p.getSystemMessage(
				header.Coinbase,
				common.HexToAddress(systemcontracts.AnchorContract),
				data,
				common.Big0,
			)

			if err = p.applyTransaction(msg, state, header, cx, &txs, &receipts, nil, &header.GasUsed, true); err != nil {
				log.Error("Submit BurnProof Failed", "number", header.Number, "burnReqId", proof.Index, "reqHash", proof.Hash)
				return nil, nil, errors.New("submit burn proofs failed")
			}
		}
	}

	if err := p.handleBlockSubscriber(state, header, cx, &txs, &receipts, nil, &header.GasUsed, true); err != nil {
		log.Warn("Farm HandleBlockSubscriber Error", err.Error())
	}

	// hande token exchange
	exTxs, err := p.anchorContract.l1ExchangesOfBlockNumber(p.config.AnchorBlockNumber(header.Number.Uint64()))
	if err != nil {
		return nil, nil, errors.New("get anchor net exchange transaction failed")
	}
	if err = p.handleAnchorTokenExchange(exTxs, state, header, cx, &txs, &receipts, nil, &header.GasUsed, true); err != nil {
		return nil, nil, errors.New("handleAnchorTokenExchange transaction failed")
	}

	snapID := state.Snapshot()

	// handle farm
	farm := farms.NewWithAnchorNet(
		state,
		p.ethAPI,
		common.HexToAddress(systemcontracts.FarmContract),
		common.HexToAddress(systemcontracts.AddressTreeContract),
		p.mainIPC,
		p.config.AnchorBlockNumber(header.Number.Uint64()),
		&p.chainConfig.Anchor.CacheDataBase,
	)

	if err := farm.FinalizeBlock(cx, p.chainConfig, header, &txs, &receipts, true); err != nil {
		state.RevertToSnapshot(snapID)
		return nil, nil, err
	}

	// should not happen. Once happen, stop the node is better than broadcast the block
	if header.GasLimit < header.GasUsed {
		return nil, nil, errors.New("gas consumption of system txs exceed the gas limit")
	}

	header.UncleHash = types.CalcUncleHash(nil)
	var blk *types.Block
	var rootHash common.Hash
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		rootHash = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
		wg.Done()
	}()
	go func() {
		blk = types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil))
		wg.Done()
	}()
	wg.Wait()
	blk.SetRoot(rootHash)

	// Assemble and return the final block for sealing
	return blk, receipts, nil
}

// Authorize injects a private key into the consensus engine to mint new blocks
// with.
func (p *Anchor) Authorize(val common.Address, signFn SignerFn, signTxFn SignerTxFn, signTextFn SignTextFn) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.val = val
	p.signFn = signFn
	p.signTxFn = signTxFn
	p.signTextFn = signTextFn
}

// Delay Argument leftOver is the time reserved for block finalize(calculate root, distribute income...)
func (p *Anchor) Delay(chain consensus.ChainReader, header *types.Header, leftOver *time.Duration) *time.Duration {
	delay := time.Duration(p.config.Period) * time.Second
	return &delay
}

// Seal implements consensus.Engine, attempting to create a sealed block using
// the local signing credentials.
func (p *Anchor) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	header := block.Header()

	// Sealing the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}

	// Don't hold the val fields for the entire sealing procedure
	p.lock.RLock()
	val, signFn := p.val, p.signFn
	p.lock.RUnlock()

	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	// Bail out if we're unauthorized to sign a block
	if _, authorized := snap.Validators[val]; !authorized {
		return errUnauthorizedValidator
	}

	// If we're amongst the recent signers, wait for the next block
	for seen, recent := range snap.Recents {
		if recent == val {
			// Signer is among recents, only wait if the current block doesn't shift it out
			if limit := uint64(len(snap.Validators)/2 + 1); number < limit || seen > number-limit {
				log.Info("Signed recently, must wait for others")
				return nil
			}
		}
	}

	// Sweet, the protocol permits us to sign the block, wait for our time
	delay := p.delayForRamanujanFork(snap, header)

	log.Info("Sealing block with", "number", number, "delay", delay, "headerDifficulty", header.Difficulty, "val", val.Hex())

	// Sign all the things!
	sig, err := signFn(accounts.Account{Address: val}, accounts.MimetypeAnchor, AnchorRLP(header, p.chainConfig.ChainID))
	if err != nil {
		return err
	}
	copy(header.Extra[len(header.Extra)-extraSeal:], sig)

	// Wait until sealing is terminated or delay timeout.
	log.Trace("Waiting for slot to sign and propagate", "delay", common.PrettyDuration(delay))
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}
		if p.shouldWaitForCurrentBlockProcess(chain, header, snap) {
			log.Info("Waiting for received in turn block to process")
			select {
			case <-stop:
				log.Info("Received block process finished, abort block seal")
				return
			case <-time.After(time.Duration(processBackOffTime) * time.Second):
				log.Info("Process backoff time exhausted, start to seal block")
			}
		}

		select {
		case results <- block.WithSeal(header):
		default:
			log.Warn("Sealing result is not read by miner", "sealhash", SealHash(header, p.chainConfig.ChainID))
		}
	}()

	return nil
}

func (p *Anchor) shouldWaitForCurrentBlockProcess(chain consensus.ChainHeaderReader, header *types.Header, snap *Snapshot) bool {
	if header.Difficulty.Cmp(diffInTurn) == 0 {
		return false
	}

	highestVerifiedHeader := chain.GetHighestVerifiedHeader()
	if highestVerifiedHeader == nil {
		return false
	}

	if header.ParentHash == highestVerifiedHeader.ParentHash {
		return true
	}
	return false
}

func (p *Anchor) EnoughDistance(chain consensus.ChainReader, header *types.Header) bool {
	snap, err := p.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, nil)
	if err != nil {
		return true
	}
	return snap.enoughDistance(p.val, header)
}

func (p *Anchor) AllowLightProcess(chain consensus.ChainReader, currentHeader *types.Header) bool {
	snap, err := p.snapshot(chain, currentHeader.Number.Uint64()-1, currentHeader.ParentHash, nil)
	if err != nil {
		return true
	}

	idx := snap.indexOfVal(p.val)
	// validator is not allowed to diff sync
	return idx < 0
}

func (p *Anchor) IsLocalBlock(header *types.Header) bool {
	return p.val == header.Coinbase
}

func (p *Anchor) SignRecently(chain consensus.ChainReader, parent *types.Block) (bool, error) {
	snap, err := p.snapshot(chain, parent.NumberU64(), parent.Hash(), nil)
	if err != nil {
		return true, err
	}

	// Bail out if we're unauthorized to sign a block
	if _, authorized := snap.Validators[p.val]; !authorized {
		return true, errUnauthorizedValidator
	}

	// If we're amongst the recent signers, wait for the next block
	number := parent.NumberU64() + 1
	for seen, recent := range snap.Recents {
		if recent == p.val {
			// Signer is among recents, only wait if the current block doesn't shift it out
			if limit := uint64(len(snap.Validators)/2 + 1); number < limit || seen > number-limit {
				return true, nil
			}
		}
	}
	return false, nil
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have based on the previous blocks in the chain and the
// current signer.
func (p *Anchor) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	snap, err := p.snapshot(chain, parent.Number.Uint64(), parent.Hash(), nil)
	if err != nil {
		return nil
	}
	return CalcDifficulty(snap, p.val)
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have based on the previous blocks in the chain and the
// current signer.
func CalcDifficulty(snap *Snapshot, signer common.Address) *big.Int {
	if snap.inturn(signer) {
		return new(big.Int).Set(diffInTurn)
	}
	return new(big.Int).Set(diffNoTurn)
}

// SealHash returns the hash of a block prior to it being sealed.
func (p *Anchor) SealHash(header *types.Header) common.Hash {
	return SealHash(header, p.chainConfig.ChainID)
}

// APIs implements consensus.Engine, returning the user facing RPC API to query snapshot.
func (p *Anchor) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	return []rpc.API{{
		Namespace: "anchor",
		Version:   "1.0",
		Service:   &API{chain: chain, anchor: p},
		Public:    false,
	}}
}

// Close implements consensus.Engine. It's a noop for parlia as there are no background threads.
func (p *Anchor) Close() error {
	return nil
}

// ==========================  interaction with contract/account =========
func (p *Anchor) handleBlockSubscriber(state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	method := "handleBlock"
	data, err := p.systemDaoABI.Pack(method)
	if err != nil {
		return err
	}

	msg := p.getSystemMessage(
		header.Coinbase,
		common.HexToAddress(systemcontracts.SystemDaoContract),
		data,
		common.Big0,
	)

	if err = p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining); err != nil {
		return err
	}
	return nil
}

// getGasPrice get current dao gas price
func (p *Anchor) getBaseGasPrice(blockHash common.Hash) (*big.Int, error) {
	// block
	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)

	// method
	method := "baseGasPrice"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	data, err := p.systemDaoABI.Pack(method)
	if err != nil {
		panic(err)
	}
	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.SystemDaoContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := p.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, blockNr, nil)
	if err != nil {
		panic(err)
	}

	var ret0 *big.Int
	if err := p.systemDaoABI.UnpackIntoInterface(&ret0, method, result); err != nil {
		return nil, err
	}
	return ret0, nil
}

func (p *Anchor) handleAnchorTokenExchange(exTxs *[]L1ExchangeTransaction,
	state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) (err error) {

	if len(*exTxs) <= 0 {
		return nil
	}

	// method
	method := "anchorTokenFrom"
	originReceiptLen := len(*receipts)

	for _, tx := range *exTxs {
		// get packed data
		data, err := p.anchorABI.Pack(method, tx)
		if err != nil {
			log.Warn("Unable to pack tx for anchorTokenFrom", "error", err)
			continue
		}
		// get system message
		msg := p.getSystemMessage(
			header.Coinbase,
			common.HexToAddress(systemcontracts.AnchorContract),
			data,
			common.Big0,
		)
		err = p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining)
		if err != nil {
			log.Warn("Handle AnchorTokenFrom Execution Reverted", "fromToken", tx.FromToken, "toToken", tx.ToToken, "amount", tx.Amount)
		}
	}

	type ReceivedEvent struct {
		Amount *big.Int
	}
	for i := originReceiptLen; i < len(*receipts); i++ {
		for _, l := range (*receipts)[i].Logs {
			if l.Topics[0] == p.anchorABI.Events["MintCoin"].ID {
				var ev ReceivedEvent
				//fromToken := common.HashToAddress(l.Topics[1])
				toAddress := common.HashToAddress(l.Topics[2])
				err := p.anchorABI.UnpackIntoInterface(&ev, "MintCoin", l.Data)
				if err != nil {
					log.Warn("Unable to pack tx for anchorTokenFrom", "error", err)
					continue
				}
				state.AddBalance(toAddress, ev.Amount)
			}
		}
	}

	return nil
}

// isBlackAddress check if given address is in black list
func (p *Anchor) isBlackAddress(blockHash common.Hash, blockNumber *big.Int, addr common.Address) (bool, error) {
	// block
	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)

	// method
	method := "isBlack"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	data, err := p.systemDaoABI.Pack(method, addr)
	if err != nil {
		log.Error("Unable to pack tx for getGasPrice", "error", err)
		return false, err
	}
	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.SystemDaoContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := p.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, blockNr, nil)
	if err != nil {
		return false, err
	}

	var ret0 bool
	if err := p.systemDaoABI.UnpackIntoInterface(&ret0, method, result); err != nil {
		return false, err
	}
	return ret0, nil
}

// slash spoiled validators
func (p *Anchor) slash(spoiledVal common.Address, state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	return nil
}

// init contract
func (p *Anchor) initContract(state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	// method
	method := "initialize"
	// contracts
	contracts := []string{
		systemcontracts.SystemDaoContract,
		systemcontracts.FarmContract,
		systemcontracts.AnchorContract,
	}
	// get packed data
	data, err := p.systemDaoABI.Pack(method, p.config.ManagerAddress)
	if err != nil {
		log.Error("Unable to pack tx for init system contract", "error", err)
		return err
	}
	for _, c := range contracts {
		msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(c), data, common.Big0)
		// apply message
		log.Info("Init inner system contract", "block hash", header.Hash(), "contract", c)
		err = p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining)
		if err != nil {
			return err
		}
	}

	return nil
}

// get system message
func (p *Anchor) getSystemMessage(from, toAddress common.Address, data []byte, value *big.Int) InteractiveCallMsg {
	return InteractiveCallMsg{
		CallMsg: ethereum.CallMsg{
			From:     from,
			Gas:      math.MaxUint64 / 2,
			GasPrice: big.NewInt(0),
			Value:    value,
			To:       &toAddress,
			Data:     data,
		},
	}
}

func (p *Anchor) applyTransaction(
	msg InteractiveCallMsg,
	state *state.StateDB,
	header *types.Header,
	chainContext core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt,
	receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool,
) (err error) {
	nonce := state.GetNonce(msg.From())
	expectedTx := types.NewTransaction(nonce, *msg.To(), msg.Value(), msg.Gas(), msg.GasPrice(), msg.Data())
	expectedHash := p.signer.Hash(expectedTx)

	if msg.From() == p.val && mining {
		expectedTx, err = p.signTxFn(accounts.Account{Address: msg.From()}, expectedTx, p.chainConfig.ChainID)
		if err != nil {
			return err
		}
	} else {
		if receivedTxs == nil || len(*receivedTxs) == 0 || (*receivedTxs)[0] == nil {
			return errors.New("supposed to get a actual transaction, but get none")
		}
		actualTx := (*receivedTxs)[0]
		if !bytes.Equal(p.signer.Hash(actualTx).Bytes(), expectedHash.Bytes()) {
			return fmt.Errorf("expected tx hash %v, get %v, nonce %d, to %s, value %s, gas %d, gasPrice %s, data %s", expectedHash.String(), actualTx.Hash().String(),
				expectedTx.Nonce(),
				expectedTx.To().String(),
				expectedTx.Value().String(),
				expectedTx.Gas(),
				expectedTx.GasPrice().String(),
				hex.EncodeToString(expectedTx.Data()),
			)
		}
		expectedTx = actualTx
		// move to next
		*receivedTxs = (*receivedTxs)[1:]
	}
	state.Prepare(expectedTx.Hash(), len(*txs))
	gasUsed, err := ApplyMessage(msg, state, header, p.chainConfig, chainContext)
	if err != nil {
		return err
	}
	*txs = append(*txs, expectedTx)
	var root []byte
	if p.chainConfig.IsByzantium(header.Number) {
		state.Finalise(true)
	} else {
		root = state.IntermediateRoot(p.chainConfig.IsEIP158(header.Number)).Bytes()
	}
	*usedGas += gasUsed
	receipt := types.NewReceipt(root, false, *usedGas)
	receipt.TxHash = expectedTx.Hash()
	receipt.GasUsed = gasUsed

	// Set the receipt logs and create a bloom for filtering
	receipt.Logs = state.GetLogs(expectedTx.Hash(), header.Hash())
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = header.Hash()
	receipt.BlockNumber = header.Number
	receipt.TransactionIndex = uint(state.TxIndex())
	*receipts = append(*receipts, receipt)
	state.SetNonce(msg.From(), nonce+1)
	return nil
}

// ===========================     utility function        ==========================
// SealHash returns the hash of a block prior to it being sealed.
func SealHash(header *types.Header, chainId *big.Int) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()
	encodeSigHeader(hasher, header, chainId)
	hasher.Sum(hash[:0])
	return hash
}

func encodeSigHeader(w io.Writer, header *types.Header, chainId *big.Int) {
	err := rlp.Encode(w, []interface{}{
		chainId,
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra[:len(header.Extra)-65], // this will panic if extra is too short, should check before calling encodeSigHeader
		header.MixDigest,
		header.Nonce,
	})
	if err != nil {
		panic("can't encode: " + err.Error())
	}
}

func backOffTime(snap *Snapshot, val common.Address) uint64 {
	if snap.inturn(val) {
		return 0
	} else {
		idx := snap.indexOfVal(val)
		if idx < 0 {
			// The backOffTime does not matter when a validator is not authorized.
			return 0
		}
		s := rand.NewSource(int64(snap.Number))
		r := rand.New(s)
		n := len(snap.Validators)
		backOffSteps := make([]uint64, 0, n)
		for idx := uint64(0); idx < uint64(n); idx++ {
			backOffSteps = append(backOffSteps, idx)
		}
		r.Shuffle(n, func(i, j int) {
			backOffSteps[i], backOffSteps[j] = backOffSteps[j], backOffSteps[i]
		})
		delay := initialBackOffTime + backOffSteps[idx]*wiggleTime
		return delay
	}
}
