// Copyright 2015 The go-ethereum Authors
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

package miner

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/consensus/anchor"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	lru "github.com/hashicorp/golang-lru"
)

const (
	// resultQueueSize is the size of channel listening to sealing result.
	resultQueueSize = 10

	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096

	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10

	// chainSideChanSize is the size of channel listening to ChainSideEvent.
	chainSideChanSize = 10

	// sealingLogAtDepth is the number of confirmations before logging successful mining.
	sealingLogAtDepth = 11

	// minRecommitInterval is the minimal time interval to recreate the sealing block with
	// any newly arrived transactions.
	minRecommitInterval = 1 * time.Second

	// staleThreshold is the maximum depth of the acceptable stale block.
	staleThreshold = 11

	// the current 4 mining loops could have asynchronous risk of mining block with
	// save height, keep recently mined blocks to avoid double sign for safety,
	recentMinedCacheLimit = 20
)

var (
	writeBlockTimer    = metrics.NewRegisteredTimer("worker/writeblock", nil)
	finalizeBlockTimer = metrics.NewRegisteredTimer("worker/finalizeblock", nil)

	errBlockInterruptedByNewHead  = errors.New("new head arrived while building block")
	errBlockInterruptedByRecommit = errors.New("recommit interrupt while building block")
	errBlockInterruptedByTimeout  = errors.New("timeout while building block")
	errBlockInterruptedByOutOfGas = errors.New("out of gas while building block")
)

// environment is the worker's current environment and holds all
// information of the sealing block generation.
type environment struct {
	signer types.Signer

	state     *state.StateDB // apply state changes here
	ancestors mapset.Set     // ancestor set (used for checking uncle parent validity)
	family    mapset.Set     // family set (used for checking uncle invalidity)
	tcount    int            // tx count in cycle
	gasPool   *core.GasPool  // available gas used to pack transactions
	coinbase  common.Address

	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt
	uncles   map[common.Hash]*types.Header
}

// copy creates a deep copy of environment.
func (env *environment) copy() *environment {
	cpy := &environment{
		signer:    env.signer,
		state:     env.state.Copy(),
		ancestors: env.ancestors.Clone(),
		family:    env.family.Clone(),
		tcount:    env.tcount,
		coinbase:  env.coinbase,
		header:    types.CopyHeader(env.header),
		receipts:  copyReceipts(env.receipts),
	}
	if env.gasPool != nil {
		gasPool := *env.gasPool
		cpy.gasPool = &gasPool
	}
	// The content of txs and uncles are immutable, unnecessary
	// to do the expensive deep copy for them.
	cpy.txs = make([]*types.Transaction, len(env.txs))
	copy(cpy.txs, env.txs)
	cpy.uncles = make(map[common.Hash]*types.Header)
	for hash, uncle := range env.uncles {
		cpy.uncles[hash] = uncle
	}
	return cpy
}

// unclelist returns the contained uncles as the list format.
func (env *environment) unclelist() []*types.Header {
	var uncles []*types.Header
	for _, uncle := range env.uncles {
		uncles = append(uncles, uncle)
	}
	return uncles
}

// discard terminates the background prefetcher go-routine. It should
// always be called for all created environment instances otherwise
// the go-routine leak can happen.
func (env *environment) discard() {
	if env.state == nil {
		return
	}
	env.state.StopPrefetcher()
}

// task contains all information for consensus engine sealing and result submitting.
type task struct {
	receipts  []*types.Receipt
	state     *state.StateDB
	block     *types.Block
	createdAt time.Time
}

const (
	commitInterruptNone int32 = iota
	commitInterruptNewHead
	commitInterruptResubmit
	commitInterruptTimeout
	commitInterruptOutOfGas
)

// newWorkReq represents a request for new sealing work submitting with relative interrupt notifier.
type newWorkReq struct {
	interruptCh chan int32
	timestamp   int64
}

// getWorkReq represents a request for getting a new sealing work with provided parameters.
type getWorkReq struct {
	params *generateParams
	err    error
	result chan *types.Block
}

// worker is the main object which takes care of submitting new work to consensus engine
// and gathering the sealing result.
type worker struct {
	prefetcher  core.Prefetcher
	config      *Config
	chainConfig *params.ChainConfig
	engine      consensus.Engine
	eth         Backend
	chain       *core.BlockChain

	// Feeds
	pendingLogsFeed event.Feed

	// Subscriptions
	mux          *event.TypeMux
	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription
	chainSideCh  chan core.ChainSideEvent
	chainSideSub event.Subscription

	// Channels
	newWorkCh          chan *newWorkReq
	getWorkCh          chan *getWorkReq
	taskCh             chan *task
	resultCh           chan *types.Block
	startCh            chan struct{}
	exitCh             chan struct{}
	resubmitIntervalCh chan time.Duration

	wg sync.WaitGroup

	current      *environment                 // An environment for current running cycle.
	localUncles  map[common.Hash]*types.Block // A set of side blocks generated locally as the possible uncle blocks.
	remoteUncles map[common.Hash]*types.Block // A set of side blocks as the possible uncle blocks.
	unconfirmed  *unconfirmedBlocks           // A set of locally mined blocks pending canonicalness confirmations.

	mu       sync.RWMutex // The lock used to protect the coinbase and extra fields
	coinbase common.Address
	extra    []byte

	pendingMu    sync.RWMutex
	pendingTasks map[common.Hash]*task

	snapshotMu       sync.RWMutex // The lock used to protect the snapshots below
	snapshotBlock    *types.Block
	snapshotReceipts types.Receipts
	snapshotState    *state.StateDB

	// atomic status counters
	running int32 // The indicator whether the consensus engine is running or not.

	// External functions
	isLocalBlock func(header *types.Header) bool // Function used to determine whether the specified block is mined by local miner.

	// Test hooks
	newTaskHook       func(*task)                        // Method to call upon receiving a new sealing task.
	skipSealHook      func(*task) bool                   // Method to decide whether skipping the sealing.
	fullTaskHook      func()                             // Method to call before pushing the full sealing task.
	resubmitHook      func(time.Duration, time.Duration) // Method to call upon updating resubmitting interval.
	recentMinedBlocks *lru.Cache
}

func newWorker(config *Config, chainConfig *params.ChainConfig, engine consensus.Engine, eth Backend, mux *event.TypeMux, isLocalBlock func(header *types.Header) bool, init bool) *worker {
	recentMinedBlocks, _ := lru.New(recentMinedCacheLimit)
	worker := &worker{
		prefetcher:         core.NewStatePrefetcher(chainConfig, eth.BlockChain(), engine),
		config:             config,
		chainConfig:        chainConfig,
		engine:             engine,
		eth:                eth,
		mux:                mux,
		chain:              eth.BlockChain(),
		isLocalBlock:       isLocalBlock,
		localUncles:        make(map[common.Hash]*types.Block),
		remoteUncles:       make(map[common.Hash]*types.Block),
		unconfirmed:        newUnconfirmedBlocks(eth.BlockChain(), sealingLogAtDepth),
		pendingTasks:       make(map[common.Hash]*task),
		chainHeadCh:        make(chan core.ChainHeadEvent, chainHeadChanSize),
		chainSideCh:        make(chan core.ChainSideEvent, chainSideChanSize),
		newWorkCh:          make(chan *newWorkReq),
		getWorkCh:          make(chan *getWorkReq),
		taskCh:             make(chan *task),
		resultCh:           make(chan *types.Block, resultQueueSize),
		exitCh:             make(chan struct{}),
		startCh:            make(chan struct{}, 1),
		resubmitIntervalCh: make(chan time.Duration),
		recentMinedBlocks:  recentMinedBlocks,
	}
	// Subscribe events for blockchain
	worker.chainHeadSub = eth.BlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)
	worker.chainSideSub = eth.BlockChain().SubscribeChainSideEvent(worker.chainSideCh)

	// Sanitize recommit interval if the user-specified one is too short.
	recommit := worker.config.Recommit
	if recommit < minRecommitInterval {
		log.Warn("Sanitizing miner recommit interval", "provided", recommit, "updated", minRecommitInterval)
		recommit = minRecommitInterval
	}

	worker.wg.Add(4)
	go worker.mainLoop()
	go worker.newWorkLoop(recommit)
	go worker.resultLoop()
	go worker.taskLoop()

	// Submit first work to initialize pending state.
	if init {
		worker.startCh <- struct{}{}
	}
	return worker
}

// setEtherbase sets the etherbase used to initialize the block coinbase field.
func (w *worker) setEtherbase(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.coinbase = addr
}

func (w *worker) setGasCeil(ceil uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.config.GasCeil = ceil
}

// setExtra sets the content used to initialize the block extra field.
func (w *worker) setExtra(extra []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.extra = extra
}

// setRecommitInterval updates the interval for miner sealing work recommitting.
func (w *worker) setRecommitInterval(interval time.Duration) {
	select {
	case w.resubmitIntervalCh <- interval:
	case <-w.exitCh:
	}
}

// pending returns the pending state and corresponding block.
func (w *worker) pending() (*types.Block, *state.StateDB) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	if w.snapshotState == nil {
		return nil, nil
	}
	return w.snapshotBlock, w.snapshotState.Copy()
}

// pendingBlock returns pending block.
func (w *worker) pendingBlock() *types.Block {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock
}

// pendingBlockAndReceipts returns pending block and corresponding receipts.
func (w *worker) pendingBlockAndReceipts() (*types.Block, types.Receipts) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock, w.snapshotReceipts
}

// start sets the running status as 1 and triggers new work submitting.
func (w *worker) start() {
	atomic.StoreInt32(&w.running, 1)
	w.startCh <- struct{}{}
}

// stop sets the running status as 0.
func (w *worker) stop() {
	atomic.StoreInt32(&w.running, 0)
}

// isRunning returns an indicator whether worker is running or not.
func (w *worker) isRunning() bool {
	return atomic.LoadInt32(&w.running) == 1
}

// close terminates all background threads maintained by the worker.
// Note the worker does not support being closed multiple times.
func (w *worker) close() {
	atomic.StoreInt32(&w.running, 0)
	close(w.exitCh)
	w.wg.Wait()
}

// newWorkLoop is a standalone goroutine to submit new sealing work upon received events.
func (w *worker) newWorkLoop(recommit time.Duration) {
	defer w.wg.Done()
	var (
		interruptCh chan int32
		minRecommit = recommit // minimal resubmit interval specified by user.
		timestamp   int64      // timestamp for each round of sealing.
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	// commit aborts in-flight transaction execution with given signal and resubmits a new one.
	commit := func(reason int32) {
		if interruptCh != nil {
			// each commit work will have its own interruptCh to stop work with a reason
			interruptCh <- reason
			close(interruptCh)
		}
		interruptCh = make(chan int32, 1)
		select {
		case w.newWorkCh <- &newWorkReq{interruptCh: interruptCh, timestamp: timestamp}:
		case <-w.exitCh:
			return
		}
		timer.Reset(recommit)
	}
	// clearPending cleans the stale pending tasks.
	clearPending := func(number uint64) {
		w.pendingMu.Lock()
		for h, t := range w.pendingTasks {
			if t.block.NumberU64()+staleThreshold <= number {
				delete(w.pendingTasks, h)
			}
		}
		w.pendingMu.Unlock()
	}

	for {
		select {
		case <-w.startCh:
			clearPending(w.chain.CurrentBlock().NumberU64())
			timestamp = time.Now().Unix()
			commit(commitInterruptNewHead)

		case head := <-w.chainHeadCh:
			if !w.isRunning() {
				continue
			}
			clearPending(head.Block.NumberU64())
			timestamp = time.Now().Unix()
			if p, ok := w.engine.(*parlia.Parlia); ok {
				signedRecent, err := p.SignRecently(w.chain, head.Block)
				if err != nil {
					log.Info("Not allowed to propose block", "err", err)
					continue
				}
				if signedRecent {
					log.Info("Signed recently, must wait")
					continue
				}
			}

			if a, ok := w.engine.(*anchor.Anchor); ok {
				signedRecent, err := a.SignRecently(w.chain, head.Block)
				if err != nil {
					log.Info("Not allowed to propose block", "err", err)
					continue
				}
				if signedRecent {
					log.Info("Signed recently, must wait")
					continue
				}
			}

			commit(commitInterruptNewHead)

		case <-timer.C:
			// If sealing is running resubmit a new work cycle periodically to pull in
			// higher priced transactions. Disable this overhead for pending blocks.
			if w.isRunning() &&
				((w.chainConfig.Ethash != nil) ||
					(w.chainConfig.Clique != nil && w.chainConfig.Clique.Period > 0) ||
					(w.chainConfig.Parlia != nil && w.chainConfig.Parlia.Period > 0)) {
				// Short circuit if no new transaction arrives.
				commit(commitInterruptResubmit)
			}

		case interval := <-w.resubmitIntervalCh:
			// Adjust resubmit interval explicitly by user.
			if interval < minRecommitInterval {
				log.Warn("Sanitizing miner recommit interval", "provided", interval, "updated", minRecommitInterval)
				interval = minRecommitInterval
			}
			log.Info("Miner recommit interval update", "from", minRecommit, "to", interval)
			minRecommit, recommit = interval, interval

			if w.resubmitHook != nil {
				w.resubmitHook(minRecommit, recommit)
			}

		case <-w.exitCh:
			return
		}
	}
}

// mainLoop is responsible for generating and submitting sealing work based on
// the received event. It can support two modes: automatically generate task and
// submit it or return task according to given parameters for various proposes.
func (w *worker) mainLoop() {
	defer w.wg.Done()
	defer w.chainHeadSub.Unsubscribe()
	defer w.chainSideSub.Unsubscribe()
	defer func() {
		if w.current != nil {
			w.current.discard()
		}
	}()

	cleanTicker := time.NewTicker(time.Second * 10)
	defer cleanTicker.Stop()

	for {
		select {
		case req := <-w.newWorkCh:
			w.commitWork(req.interruptCh, req.timestamp)

		case req := <-w.getWorkCh:
			block, err := w.generateWork(req.params)
			if err != nil {
				req.err = err
				req.result <- nil
			} else {
				req.result <- block
			}

		case ev := <-w.chainSideCh:
			// Short circuit for duplicate side blocks
			if _, ok := w.engine.(*parlia.Parlia); ok {
				continue
			}
			if _, ok := w.engine.(*anchor.Anchor); ok {
				continue
			}
			if _, exist := w.localUncles[ev.Block.Hash()]; exist {
				continue
			}
			if _, exist := w.remoteUncles[ev.Block.Hash()]; exist {
				continue
			}
			// Add side block to possible uncle block set depending on the author.
			if w.isLocalBlock != nil && w.isLocalBlock(ev.Block.Header()) {
				w.localUncles[ev.Block.Hash()] = ev.Block
			} else {
				w.remoteUncles[ev.Block.Hash()] = ev.Block
			}
			// If our sealing block contains less than 2 uncle blocks,
			// add the new uncle block if valid and regenerate a new
			// sealing block for higher profit.
			if w.isRunning() && w.current != nil && len(w.current.uncles) < 2 {
				start := time.Now()
				if err := w.commitUncle(w.current, ev.Block.Header()); err == nil {
					w.commit(w.current, nil, false, start)
				}
			}

		case <-cleanTicker.C:
			chainHead := w.chain.CurrentBlock()
			for hash, uncle := range w.localUncles {
				if uncle.NumberU64()+staleThreshold <= chainHead.NumberU64() {
					delete(w.localUncles, hash)
				}
			}
			for hash, uncle := range w.remoteUncles {
				if uncle.NumberU64()+staleThreshold <= chainHead.NumberU64() {
					delete(w.remoteUncles, hash)
				}
			}

		// System stopped
		case <-w.exitCh:
			return
		case <-w.chainHeadSub.Err():
			return
		case <-w.chainSideSub.Err():
			return
		}
	}
}

// taskLoop is a standalone goroutine to fetch sealing task from the generator and
// push them to consensus engine.
func (w *worker) taskLoop() {
	defer w.wg.Done()
	var (
		stopCh chan struct{}
		prev   common.Hash
	)

	// interrupt aborts the in-flight sealing task.
	interrupt := func() {
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
		}
	}
	for {
		select {
		case task := <-w.taskCh:
			if w.newTaskHook != nil {
				w.newTaskHook(task)
			}
			// Reject duplicate sealing work due to resubmitting.
			sealHash := w.engine.SealHash(task.block.Header())
			if sealHash == prev {
				continue
			}
			// Interrupt previous sealing operation
			interrupt()
			stopCh, prev = make(chan struct{}), sealHash

			if w.skipSealHook != nil && w.skipSealHook(task) {
				continue
			}
			w.pendingMu.Lock()
			w.pendingTasks[sealHash] = task
			w.pendingMu.Unlock()

			if err := w.engine.Seal(w.chain, task.block, w.resultCh, stopCh); err != nil {
				log.Warn("Block sealing failed", "err", err)
				w.pendingMu.Lock()
				delete(w.pendingTasks, sealHash)
				w.pendingMu.Unlock()
			}
		case <-w.exitCh:
			interrupt()
			return
		}
	}
}

// resultLoop is a standalone goroutine to handle sealing result submitting
// and flush relative data to the database.
func (w *worker) resultLoop() {
	defer w.wg.Done()
	for {
		select {
		case block := <-w.resultCh:
			// Short circuit when receiving empty result.
			if block == nil {
				continue
			}
			// Short circuit when receiving duplicate result caused by resubmitting.
			if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
				continue
			}
			var (
				sealhash = w.engine.SealHash(block.Header())
				hash     = block.Hash()
			)
			w.pendingMu.RLock()
			task, exist := w.pendingTasks[sealhash]
			w.pendingMu.RUnlock()
			if !exist {
				log.Error("Block found but no relative pending task", "number", block.Number(), "sealhash", sealhash, "hash", hash)
				continue
			}
			// Different block could share same sealhash, deep copy here to prevent write-write conflict.
			var (
				receipts = make([]*types.Receipt, len(task.receipts))
				logs     []*types.Log
			)
			for i, taskReceipt := range task.receipts {
				receipt := new(types.Receipt)
				receipts[i] = receipt
				*receipt = *taskReceipt

				// add block location fields
				receipt.BlockHash = hash
				receipt.BlockNumber = block.Number()
				receipt.TransactionIndex = uint(i)

				// Update the block hash in all logs since it is now available and not when the
				// receipt/log of individual transactions were created.
				receipt.Logs = make([]*types.Log, len(taskReceipt.Logs))
				for i, taskLog := range taskReceipt.Logs {
					log := new(types.Log)
					receipt.Logs[i] = log
					*log = *taskLog
					log.BlockHash = hash
				}
				logs = append(logs, receipt.Logs...)
			}

			if prev, ok := w.recentMinedBlocks.Get(block.NumberU64()); ok {
				doubleSign := false
				prevParents, _ := prev.([]common.Hash)
				for _, prevParent := range prevParents {
					if prevParent == block.ParentHash() {
						log.Error("Reject Double Sign!!", "block", block.NumberU64(),
							"hash", block.Hash(),
							"root", block.Root(),
							"ParentHash", block.ParentHash())
						doubleSign = true
						break
					}
				}
				if doubleSign {
					continue
				}
				prevParents = append(prevParents, block.ParentHash())
				w.recentMinedBlocks.Add(block.NumberU64(), prevParents)
			} else {
				// Add() will call removeOldest internally to remove the oldest element
				// if the LRU Cache is full
				w.recentMinedBlocks.Add(block.NumberU64(), []common.Hash{block.ParentHash()})
			}

			// Broadcast the block and announce chain insertion event
			w.mux.Post(core.NewMinedBlockEvent{Block: block})

			// Commit block and state to database.
			task.state.SetExpectedStateRoot(block.Root())
			start := time.Now()
			_, err := w.chain.WriteBlockAndSetHead(block, receipts, logs, task.state, true)
			if err != nil {
				log.Error("Failed writing block to chain", "err", err)
				continue
			}
			writeBlockTimer.UpdateSince(start)
			log.Info("Successfully sealed new block", "number", block.Number(), "sealhash", sealhash, "hash", hash,
				"elapsed", common.PrettyDuration(time.Since(task.createdAt)))

			// Insert the block into the set of pending ones to resultLoop for confirmations
			w.unconfirmed.Insert(block.NumberU64(), block.Hash())

		case <-w.exitCh:
			return
		}
	}
}

// makeEnv creates a new environment for the sealing block.
func (w *worker) makeEnv(parent *types.Block, header *types.Header, coinbase common.Address,
	prevEnv *environment) (*environment, error) {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit
	state, err := w.chain.StateAtWithSharedPool(parent.Root())
	if err != nil {
		return nil, err
	}
	if prevEnv == nil {
		state.StartPrefetcher("miner")
	} else {
		state.TransferPrefetcher(prevEnv.state)
	}

	// Note the passed coinbase may be different with header.Coinbase.
	env := &environment{
		signer:    types.MakeSigner(w.chainConfig, header.Number),
		state:     state,
		coinbase:  coinbase,
		ancestors: mapset.NewSet(),
		family:    mapset.NewSet(),
		header:    header,
		uncles:    make(map[common.Hash]*types.Header),
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0
	return env, nil
}

// commitUncle adds the given block to uncle block set, returns error if failed to add.
func (w *worker) commitUncle(env *environment, uncle *types.Header) error {
	if w.isTTDReached(env.header) {
		return errors.New("ignore uncle for beacon block")
	}
	hash := uncle.Hash()
	if _, exist := env.uncles[hash]; exist {
		return errors.New("uncle not unique")
	}
	if env.header.ParentHash == uncle.ParentHash {
		return errors.New("uncle is sibling")
	}
	if !env.ancestors.Contains(uncle.ParentHash) {
		return errors.New("uncle's parent unknown")
	}
	if env.family.Contains(hash) {
		return errors.New("uncle already included")
	}
	env.uncles[hash] = uncle
	return nil
}

// updateSnapshot updates pending snapshot block, receipts and state.
func (w *worker) updateSnapshot(env *environment) {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	w.snapshotBlock = types.NewBlock(
		env.header,
		env.txs,
		env.unclelist(),
		env.receipts,
		trie.NewStackTrie(nil),
	)
	w.snapshotReceipts = copyReceipts(env.receipts)
	w.snapshotState = env.state.Copy()
}

func (w *worker) commitTransaction(env *environment, tx *types.Transaction, receiptProcessors ...core.ReceiptProcessor) ([]*types.Log, error) {
	snap := env.state.Snapshot()

	receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, &env.coinbase, env.gasPool, env.state, env.header, tx, &env.header.GasUsed, *w.chain.GetVMConfig(), receiptProcessors...)
	if err != nil {
		env.state.RevertToSnapshot(snap)
		return nil, err
	}
	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) commitTransactions(env *environment, txs *types.TransactionsByPriceAndNonce,
	interruptCh chan int32, stopTimer *time.Timer) error {
	gasLimit := env.header.GasLimit
	if env.gasPool == nil {
		env.gasPool = new(core.GasPool).AddGas(gasLimit)
		if w.chain.Config().IsEuler(env.header.Number) {
			env.gasPool.SubGas(params.SystemTxsGas * 3)
		} else {
			env.gasPool.SubGas(params.SystemTxsGas)
		}
	}

	var coalescedLogs []*types.Log
	// initialize bloom processors
	processorCapacity := 100
	if txs.CurrentSize() < processorCapacity {
		processorCapacity = txs.CurrentSize()
	}
	bloomProcessors := core.NewAsyncReceiptBloomGenerator(processorCapacity)

	stopPrefetchCh := make(chan struct{})
	defer close(stopPrefetchCh)
	//prefetch txs from all pending txs
	txsPrefetch := txs.Copy()
	tx := txsPrefetch.Peek()
	txCurr := &tx
	w.prefetcher.PrefetchMining(txsPrefetch, env.header, env.gasPool.Gas(), env.state.CopyDoPrefetch(), *w.chain.GetVMConfig(), stopPrefetchCh, txCurr)

	signal := commitInterruptNone
LOOP:
	for {
		// In the following three cases, we will interrupt the execution of the transaction.
		// (1) new head block event arrival, the reason is 1
		// (2) worker start or restart, the reason is 1
		// (3) worker recreate the sealing block with any newly arrived transactions, the reason is 2.
		// For the first two cases, the semi-finished work will be discarded.
		// For the third case, the semi-finished work will be submitted to the consensus engine.
		if interruptCh != nil {
			select {
			case signal, ok := <-interruptCh:
				if !ok {
					// should never be here, since interruptCh should not be read before
					log.Warn("commit transactions stopped unknown")
				}
				return signalToErr(signal)
			default:
			}
		}
		// If we don't have enough gas for any further transactions then we're done
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			signal = commitInterruptOutOfGas
			break
		}
		if stopTimer != nil {
			select {
			case <-stopTimer.C:
				log.Info("Not enough time for further transactions", "txs", len(env.txs))
				stopTimer.Reset(0) // re-active the timer, in case it will be used later.
				signal = commitInterruptTimeout
				break LOOP
			default:
			}
		}
		// Retrieve the next transaction and abort if all done
		tx = txs.Peek()
		if tx == nil {
			break
		}
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		//from, _ := types.Sender(env.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(env.header.Number) {
			//log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)
			txs.Pop()
			continue
		}
		//todo blacklist verification

		// Start executing the transaction
		env.state.Prepare(tx.Hash(), env.tcount)

		logs, err := w.commitTransaction(env, tx, bloomProcessors)
		switch {
		case errors.Is(err, core.ErrGasLimitReached):
			// Pop the current out-of-gas transaction without shifting in the next from the account
			//log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			//log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, core.ErrNonceTooHigh):
			// Reorg notification data race between the transaction pool and miner, skip account =
			//log.Trace("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
			txs.Pop()

		case errors.Is(err, nil):
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
			txs.Shift()

		case errors.Is(err, core.ErrTxTypeNotSupported):
			// Pop the unsupported transaction without shifting in the next from the account
			//log.Trace("Skipping unsupported transaction type", "sender", from, "type", tx.Type())
			txs.Pop()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			//log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}
	bloomProcessors.Close()
	if !w.isRunning() && len(coalescedLogs) > 0 {
		// We don't push the pendingLogsEvent while we are sealing. The reason is that
		// when we are sealing, the worker will regenerate a sealing block every 3 seconds.
		// In order to avoid pushing the repeated pendingLog, we disable the pending log pushing.

		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		w.pendingLogsFeed.Send(cpy)
	}
	return signalToErr(signal)
}

// generateParams wraps various of settings for generating sealing task.
type generateParams struct {
	timestamp  uint64         // The timstamp for sealing task
	forceTime  bool           // Flag whether the given timestamp is immutable or not
	parentHash common.Hash    // Parent block hash, empty means the latest chain head
	coinbase   common.Address // The fee recipient address for including transaction
	random     common.Hash    // The randomness generated by beacon chain, empty before the merge
	noUncle    bool           // Flag whether the uncle block inclusion is allowed
	noExtra    bool           // Flag whether the extra field assignment is allowed
	prevWork   *environment
}

// prepareWork constructs the sealing task according to the given parameters,
// either based on the last chain head or specified parent. In this function
// the pending transactions are not filled yet, only the empty task returned.
func (w *worker) prepareWork(genParams *generateParams) (*environment, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Find the parent block for sealing task
	parent := w.chain.CurrentBlock()
	if genParams.parentHash != (common.Hash{}) {
		parent = w.chain.GetBlockByHash(genParams.parentHash)
	}
	if parent == nil {
		return nil, fmt.Errorf("missing parent")
	}
	// Sanity check the timestamp correctness, recap the timestamp
	// to parent+1 if the mutation is allowed.
	timestamp := genParams.timestamp
	if parent.Time() >= timestamp {
		if genParams.forceTime {
			return nil, fmt.Errorf("invalid timestamp, parent %d given %d", parent.Time(), timestamp)
		}
		timestamp = parent.Time() + 1
	}
	// Construct the sealing block header, set the extra field if it's allowed
	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit(), w.config.GasCeil),
		Time:       timestamp,
		Coinbase:   genParams.coinbase,
	}
	if !genParams.noExtra && len(w.extra) != 0 {
		header.Extra = w.extra
	}
	// Set the randomness field from the beacon chain if it's available.
	if genParams.random != (common.Hash{}) {
		header.MixDigest = genParams.random
	}
	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	//if w.chainConfig.IsLondon(header.Number) {
	//	header.BaseFee = misc.CalcBaseFee(w.chainConfig, parent.Header())
	//	if !w.chainConfig.IsLondon(parent.Number()) {
	//		parentGasLimit := parent.GasLimit() * params.ElasticityMultiplier
	//		header.GasLimit = core.CalcGasLimit(parentGasLimit, w.config.GasCeil)
	//	}
	//}
	// Run the consensus preparation with the default or customized consensus engine.
	if err := w.engine.Prepare(w.chain, header); err != nil {
		log.Error("Failed to prepare header for sealing", "err", err)
		return nil, err
	}

	// Could potentially happen if starting to mine in an odd state.
	// Note genParams.coinbase can be different with header.Coinbase
	// since clique algorithm can modify the coinbase field in header.
	env, err := w.makeEnv(parent, header, genParams.coinbase, genParams.prevWork)
	if err != nil {
		log.Error("Failed to create sealing context", "err", err)
		return nil, err
	}

	// Handle upgrade build-in system contract code
	systemcontracts.UpgradeBuildInSystemContract(w.chainConfig, header.Number, env.state)

	// Accumulate the uncles for the sealing work only if it's allowed.
	if !genParams.noUncle {
		commitUncles := func(blocks map[common.Hash]*types.Block) {
			for hash, uncle := range blocks {
				if len(env.uncles) == 2 {
					break
				}
				if err := w.commitUncle(env, uncle.Header()); err != nil {
					log.Trace("Possible uncle rejected", "hash", hash, "reason", err)
				} else {
					log.Debug("Committing new uncle to block", "hash", hash)
				}
			}
		}
		// Prefer to locally generated uncle
		commitUncles(w.localUncles)
		commitUncles(w.remoteUncles)
	}
	return env, nil
}

// fillTransactions retrieves the pending transactions from the txpool and fills them
// into the given sealing block. The transaction selection and ordering strategy can
// be customized with the plugin in the future.
func (w *worker) fillTransactions(interruptCh chan int32, env *environment, stopTimer *time.Timer) (err error) {
	// Split the pending transactions into locals and remotes
	// Fill the block with all available pending transactions.
	pending := w.eth.TxPool().Pending(false)
	localTxs, remoteTxs := make(map[common.Address]types.Transactions), pending
	for _, account := range w.eth.TxPool().Locals() {
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}

	err = nil
	if len(localTxs) > 0 {
		txs := types.NewTransactionsByPriceAndNonce(env.signer, localTxs, env.header.BaseFee)
		err = w.commitTransactions(env, txs, interruptCh, stopTimer)
		// we will abort here when:
		//   1.new block was imported
		//   2.out of Gas, no more transaction can be added.
		//   3.the mining timer has expired, stop adding transactions.
		//   4.interrupted resubmit timer, which is by default 10s.
		//     resubmit is for PoW only, can be deleted for PoS consensus later
		if err != nil {
			return
		}
	}
	if len(remoteTxs) > 0 {
		txs := types.NewTransactionsByPriceAndNonce(env.signer, remoteTxs, env.header.BaseFee)
		err = w.commitTransactions(env, txs, interruptCh, stopTimer)
	}

	return
}

// generateWork generates a sealing block based on the given parameters.
func (w *worker) generateWork(params *generateParams) (*types.Block, error) {
	work, err := w.prepareWork(params)
	if err != nil {
		return nil, err
	}
	defer work.discard()

	w.fillTransactions(nil, work, nil)
	block, _, err := w.engine.FinalizeAndAssemble(w.chain, work.header, work.state, work.txs, work.unclelist(), work.receipts)
	return block, err
}

// commitWork generates several new sealing tasks based on the parent block
// and submit them to the sealer.
func (w *worker) commitWork(interruptCh chan int32, timestamp int64) {
	start := time.Now()

	// Set the coinbase if the worker is running or it's required
	var coinbase common.Address
	if w.isRunning() {
		if w.coinbase == (common.Address{}) {
			log.Error("Refusing to mine without etherbase")
			return
		}
		coinbase = w.coinbase // Use the preset address as the fee recipient
	}

	stopTimer := time.NewTimer(0)
	defer stopTimer.Stop()
	<-stopTimer.C // discard the initial tick

	stopWaitTimer := time.NewTimer(0)
	defer stopWaitTimer.Stop()
	<-stopWaitTimer.C // discard the initial tick

	// validator can try several times to get the most profitable block,
	// as long as the timestamp is not reached.
	workList := make([]*environment, 0, 10)
	var prevWork *environment
	// workList clean up
	defer func() {
		for _, wk := range workList {
			// only keep the best work, discard others.
			if wk == w.current {
				continue
			}
			wk.discard()
		}
	}()

LOOP:
	for {
		work, err := w.prepareWork(&generateParams{
			timestamp: uint64(timestamp),
			coinbase:  coinbase,
			prevWork:  prevWork,
		})
		if err != nil {
			return
		}
		prevWork = work
		workList = append(workList, work)

		delay := w.engine.Delay(w.chain, work.header, &w.config.DelayLeftOver)
		if delay == nil {
			log.Warn("commitWork delay is nil, something is wrong")
			stopTimer = nil
		} else if *delay <= 0 {
			log.Debug("Not enough time for commitWork")
			break
		} else {
			log.Debug("commitWork stopTimer", "block", work.header.Number,
				"header time", time.Until(time.Unix(int64(work.header.Time), 0)),
				"commit delay", *delay, "DelayLeftOver", w.config.DelayLeftOver)
			stopTimer.Reset(*delay)
		}

		// subscribe before fillTransactions
		txsCh := make(chan core.NewTxsEvent, txChanSize)
		sub := w.eth.TxPool().SubscribeNewTxsEvent(txsCh)
		defer sub.Unsubscribe()

		// Fill pending transactions from the txpool
		fillStart := time.Now()
		err = w.fillTransactions(interruptCh, work, stopTimer)
		fillDuration := time.Since(fillStart)
		switch {
		case errors.Is(err, errBlockInterruptedByNewHead):
			log.Debug("commitWork abort", "err", err)
			return
		case errors.Is(err, errBlockInterruptedByRecommit):
		case errors.Is(err, errBlockInterruptedByTimeout):
		case errors.Is(err, errBlockInterruptedByOutOfGas):
			// break the loop to get the best work
			log.Debug("commitWork finish", "reason", err)
			break LOOP
		}

		if interruptCh == nil || stopTimer == nil {
			// it is single commit work, no need to try several time.
			log.Info("commitWork interruptCh or stopTimer is nil")
			break
		}

		newTxsNum := 0
		// stopTimer was the maximum delay for each fillTransactions
		// but now it is used to wait until (head.Time - DelayLeftOver) is reached.
		stopTimer.Reset(time.Until(time.Unix(int64(work.header.Time), 0)) - w.config.DelayLeftOver)
	LOOP_WAIT:
		for {
			select {
			case <-stopTimer.C:
				log.Debug("commitWork stopTimer expired")
				break LOOP
			case <-interruptCh:
				log.Debug("commitWork interruptCh closed, new block imported or resubmit triggered")
				return
			case ev := <-txsCh:
				delay := w.engine.Delay(w.chain, work.header, &w.config.DelayLeftOver)
				log.Debug("commitWork txsCh arrived", "fillDuration", fillDuration.String(),
					"delay", delay.String(), "work.tcount", work.tcount,
					"newTxsNum", newTxsNum, "len(ev.Txs)", len(ev.Txs))
				if *delay < fillDuration {
					// There may not have enough time for another fillTransactions.
					break LOOP
				} else if *delay < fillDuration*2 {
					// We can schedule another fillTransactions, but the time is limited,
					// probably it is the last chance, schedule it immediately.
					break LOOP_WAIT
				} else {
					// There is still plenty of time left.
					// We can wait a while to collect more transactions before
					// schedule another fillTransaction to reduce CPU cost.
					// There will be 2 cases to schedule another fillTransactions:
					//   1.newTxsNum >= work.tcount
					//   2.no much time left, have to schedule it immediately.
					newTxsNum = newTxsNum + len(ev.Txs)
					if newTxsNum >= work.tcount {
						break LOOP_WAIT
					}
					stopWaitTimer.Reset(*delay - fillDuration*2)
				}
			case <-stopWaitTimer.C:
				if newTxsNum > 0 {
					break LOOP_WAIT
				}
			}
		}
		// if sub's channel if full, it will block other NewTxsEvent subscribers,
		// so unsubscribe ASAP and Unsubscribe() is re-enterable, safe to call several time.
		sub.Unsubscribe()
	}
	// get the most profitable work
	bestWork := workList[0]
	bestReward := new(big.Int)
	for i, wk := range workList {
		balance := wk.state.GetBalance(consensus.SystemAddress)
		log.Debug("Get the most profitable work", "index", i, "balance", balance, "bestReward", bestReward)
		if balance.Cmp(bestReward) > 0 {
			bestWork = wk
			bestReward = balance
		}
	}
	w.commit(bestWork, w.fullTaskHook, true, start)

	// Swap out the old work with the new one, terminating any leftover
	// prefetcher processes in the mean time and starting a new one.
	if w.current != nil {
		w.current.discard()
	}
	w.current = bestWork
}

// commit runs any post-transaction state modifications, assembles the final block
// and commits new work if consensus engine is running.
// Note the assumption is held that the mutation is allowed to the passed env, do
// the deep copy first.
func (w *worker) commit(env *environment, interval func(), update bool, start time.Time) error {
	if w.isRunning() {
		if interval != nil {
			interval()
		}

		err := env.state.WaitPipeVerification()
		if err != nil {
			return err
		}
		env.state.CorrectAccountsRoot(w.chain.CurrentBlock().Root())

		finalizeStart := time.Now()
		block, receipts, err := w.engine.FinalizeAndAssemble(w.chain, types.CopyHeader(env.header), env.state, env.txs, env.unclelist(), env.receipts)
		if err != nil {
			return err
		}
		finalizeBlockTimer.UpdateSince(finalizeStart)

		// Create a local environment copy, avoid the data race with snapshot state.
		// https://github.com/ethereum/go-ethereum/issues/24299
		env := env.copy()

		// If we're post merge, just ignore
		if !w.isTTDReached(block.Header()) {
			select {
			case w.taskCh <- &task{receipts: receipts, state: env.state, block: block, createdAt: time.Now()}:
				w.unconfirmed.Shift(block.NumberU64() - 1)
				log.Info("Commit new mining work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
					"uncles", len(env.uncles), "txs", env.tcount,
					"gas", block.GasUsed(),
					"elapsed", common.PrettyDuration(time.Since(start)))

			case <-w.exitCh:
				log.Info("Worker has exited")
			}
		}
	}
	if update {
		w.updateSnapshot(env)
	}
	return nil
}

// getSealingBlock generates the sealing block based on the given parameters.
func (w *worker) getSealingBlock(parent common.Hash, timestamp uint64, coinbase common.Address, random common.Hash) (*types.Block, error) {
	req := &getWorkReq{
		params: &generateParams{
			timestamp:  timestamp,
			forceTime:  true,
			parentHash: parent,
			coinbase:   coinbase,
			random:     random,
			noUncle:    true,
			noExtra:    true,
		},
		result: make(chan *types.Block, 1),
	}
	select {
	case w.getWorkCh <- req:
		block := <-req.result
		if block == nil {
			return nil, req.err
		}
		return block, nil
	case <-w.exitCh:
		return nil, errors.New("miner closed")
	}
}

// isTTDReached returns the indicator if the given block has reached the total
// terminal difficulty for The Merge transition.
func (w *worker) isTTDReached(header *types.Header) bool {
	td, ttd := w.chain.GetTd(header.ParentHash, header.Number.Uint64()-1), w.chain.Config().TerminalTotalDifficulty
	return td != nil && ttd != nil && td.Cmp(ttd) >= 0
}

// copyReceipts makes a deep copy of the given receipts.
func copyReceipts(receipts []*types.Receipt) []*types.Receipt {
	result := make([]*types.Receipt, len(receipts))
	for i, l := range receipts {
		cpy := *l
		result[i] = &cpy
	}
	return result
}

// postSideBlock fires a side chain event, only use it for testing.
func (w *worker) postSideBlock(event core.ChainSideEvent) {
	select {
	case w.chainSideCh <- event:
	case <-w.exitCh:
	}
}

// signalToErr converts the interruption signal to a concrete error type for return.
// The given signal must be a valid interruption signal.
func signalToErr(signal int32) error {
	switch signal {
	case commitInterruptNone:
		return nil
	case commitInterruptNewHead:
		return errBlockInterruptedByNewHead
	case commitInterruptResubmit:
		return errBlockInterruptedByRecommit
	case commitInterruptTimeout:
		return errBlockInterruptedByTimeout
	case commitInterruptOutOfGas:
		return errBlockInterruptedByOutOfGas
	default:
		panic(fmt.Errorf("undefined signal %d", signal))
	}
}
