package blockchain

import (
	"bytes"
	"fmt"
	"time"

	"github.com/0xPolygon/polygon-edge/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/txpool"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo/contract"
	"github.com/hashicorp/go-hclog"
)

type BlockBuilder interface {
	Reset() error
	WriteTx(*types.Transaction) error
	Fill()
	Build(func(h *types.Header)) (*types.FullBlock, error)
	GetState() *state.Transition
	Receipts() []*types.Receipt
}

// blockchain is an interface that wraps the methods called on blockchain
type Blockchain interface {
	// CurrentHeader returns the header of blockchain block head
	CurrentHeader() *types.Header

	// CommitBlock commits a block to the chain.
	CommitBlock(block *types.FullBlock) error

	// NewBlockBuilder is a factory method that returns a block builder on top of 'parent'.
	NewBlockBuilder(parent *types.Header, coinbase types.Address,
		txPool TxPool, blockTime time.Duration, logger hclog.Logger) (BlockBuilder, error)

	// ProcessBlock builds a final block from given 'block' on top of 'parent'.
	ProcessBlock(parent *types.Header, block *types.Block) (*types.FullBlock, error)

	// GetStateProviderForBlock returns a reference to make queries to the state at 'block'.
	GetStateProviderForBlock(block *types.Header) (contract.Provider, error)

	// GetStateProvider returns a reference to make queries to the provided state.
	GetStateProvider(transition *state.Transition) contract.Provider

	// GetHeaderByNumber returns a reference to block header for the given block number.
	GetHeaderByNumber(number uint64) (*types.Header, bool)

	// GetHeaderByHash returns a reference to block header for the given block hash
	GetHeaderByHash(hash types.Hash) (*types.Header, bool)

	// GetSystemState creates a new instance of SystemState interface
	GetSystemState(provider contract.Provider) systemstate.SystemState

	// SubscribeEvents subscribes to blockchain events
	SubscribeEvents() blockchain.Subscription

	// UnubscribeEvents unsubscribes from blockchain events
	UnubscribeEvents(subscription blockchain.Subscription)

	// GetChainID returns chain id of the current blockchain
	GetChainID() uint64

	// GetReceiptsByHash retrieves receipts by hash
	GetReceiptsByHash(hash types.Hash) ([]*types.Receipt, error)
}

type TxPool interface {
	Prepare()
	Length() uint64
	Peek() *types.Transaction
	Pop(*types.Transaction)
	Drop(*types.Transaction)
	Demote(*types.Transaction)
	SetSealing(bool)
	ResetWithBlock(*types.Block)
	ReinsertProposed()
	ClearProposed()
}

// BlockBuilderParams are fields for the block that cannot be changed
type BlockBuilderParams struct {
	// Parent block
	Parent *types.Header

	// Executor
	Executor *state.Executor

	// Coinbase that is signing the block
	Coinbase types.Address

	// GasLimit is the gas limit for the block
	GasLimit uint64

	// duration for one block
	BlockTime time.Duration

	// Logger
	Logger hclog.Logger

	// txPoolInterface implementation
	TxPool TxPool

	// BaseFee is the base fee
	BaseFee uint64
}

func NewBlockBuilder(params *BlockBuilderParams) BlockBuilder {
	return &BlockBuilderImpl{
		params: params,
	}
}

var _ BlockBuilder = &BlockBuilderImpl{}

type BlockBuilderImpl struct {
	// input params for the block
	params *BlockBuilderParams

	// header is the header for the block being created
	header *types.Header

	// transactions are the data included in the block
	txns []*types.Transaction

	// block is a reference to the already built block
	block *types.Block

	// state is in memory state transition
	state *state.Transition
}

// Init initializes block builder before adding transactions and actual block building
func (b *BlockBuilderImpl) Reset() error {
	// set the timestamp
	parentTime := time.Unix(int64(b.params.Parent.Timestamp), 0)
	headerTime := parentTime.Add(b.params.BlockTime)

	if headerTime.Before(time.Now().UTC()) {
		headerTime = time.Now().UTC()
	}

	b.header = &types.Header{
		ParentHash:   b.params.Parent.Hash,
		Number:       b.params.Parent.Number + 1,
		Miner:        b.params.Coinbase[:],
		Difficulty:   1,
		StateRoot:    types.EmptyRootHash, // this avoids needing state for now
		TxRoot:       types.EmptyRootHash,
		ReceiptsRoot: types.EmptyRootHash, // this avoids needing state for now
		Sha3Uncles:   types.EmptyUncleHash,
		GasLimit:     b.params.GasLimit,
		BaseFee:      b.params.BaseFee,
		Timestamp:    uint64(headerTime.Unix()),
	}

	transition, err := b.params.Executor.BeginTxn(b.params.Parent.StateRoot, b.header, b.params.Coinbase)
	if err != nil {
		return err
	}

	b.state = transition
	b.block = nil
	b.txns = []*types.Transaction{}

	return nil
}

// Block returns the built block if nil, it is not built yet
func (b *BlockBuilderImpl) Block() *types.Block {
	return b.block
}

// Build creates the state and the final block
func (b *BlockBuilderImpl) Build(handler func(h *types.Header)) (*types.FullBlock, error) {
	if handler != nil {
		handler(b.header)
	}

	_, stateRoot, err := b.state.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit the state changes: %w", err)
	}

	b.header.StateRoot = stateRoot
	b.header.GasUsed = b.state.TotalGas()
	b.header.LogsBloom = types.CreateBloom(b.Receipts())

	// build the block
	b.block = consensus.BuildBlock(consensus.BuildBlockParams{
		Header:   b.header,
		Txns:     b.txns,
		Receipts: b.state.Receipts(),
	})

	b.block.Header.ComputeHash()

	return &types.FullBlock{
		Block:    b.block,
		Receipts: b.state.Receipts(),
	}, nil
}

// WriteTx applies given transaction to the state. If transaction apply fails, it reverts the saved snapshot.
func (b *BlockBuilderImpl) WriteTx(tx *types.Transaction) error {
	if tx.Gas() > b.params.GasLimit {
		b.params.Logger.Info("Transaction gas limit exceedes block gas limit", "hash", tx.Hash(),
			"tx gas limit", tx.Gas(), "block gas limt", b.params.GasLimit)

		return txpool.ErrBlockLimitExceeded
	}

	if err := b.state.Write(tx); err != nil {
		return err
	}

	b.txns = append(b.txns, tx)

	return nil
}

// Fill fills the block with transactions from the txpool
func (b *BlockBuilderImpl) Fill() {
	var buf bytes.Buffer

	blockTimer := time.NewTimer(b.params.BlockTime)

	b.params.TxPool.Prepare()
write:
	for {
		select {
		case <-blockTimer.C:
			return
		default:
			tx := b.params.TxPool.Peek()

			if b.params.Logger.IsTrace() && tx != nil {
				_, _ = buf.WriteString(tx.String())
			}

			// execute transactions one by one
			finished, err := b.writeTxPoolTransaction(tx)
			if err != nil {
				b.params.Logger.Debug("Fill transaction error", "hash", tx.Hash(), "err", err)
			}

			if finished {
				break write
			}
		}
	}

	if b.params.Logger.IsDebug() {
		b.params.Logger.Debug("[BlockBuilder.Fill]", "block number", b.header.Number, "block txs", buf.String())
	}

	//	wait for the timer to expire
	<-blockTimer.C
}

// Receipts returns the collection of transaction receipts for given block
func (b *BlockBuilderImpl) Receipts() []*types.Receipt {
	return b.state.Receipts()
}

func (b *BlockBuilderImpl) writeTxPoolTransaction(tx *types.Transaction) (bool, error) {
	if tx == nil {
		return true, nil
	}

	if err := b.WriteTx(tx); err != nil {
		if _, ok := err.(*state.GasLimitReachedTransitionApplicationError); ok { //nolint:errorlint
			// stop processing
			return true, err
		} else if appErr, ok := err.(*state.TransitionApplicationError); ok && appErr.IsRecoverable { //nolint:errorlint
			b.params.TxPool.Demote(tx)

			return false, err
		} else {
			b.params.TxPool.Drop(tx)

			return false, err
		}
	}

	// remove tx from the pool and add it to the list of all block transactions
	b.params.TxPool.Pop(tx)

	return false, nil
}

// GetState returns Transition reference
func (b *BlockBuilderImpl) GetState() *state.Transition {
	return b.state
}
