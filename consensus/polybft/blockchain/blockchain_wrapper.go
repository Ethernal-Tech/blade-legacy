package blockchain

import (
	"fmt"
	"time"

	"github.com/hashicorp/go-hclog"

	"github.com/0xPolygon/polygon-edge/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/metrics"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo/contract"
)

const (
	consensusSource = "consensus"
)

var _ Blockchain = &BlockchainWrapper{}

type BlockchainWrapper struct {
	logger     hclog.Logger
	executor   *state.Executor
	blockchain *blockchain.Blockchain
}

func NewBlockchainWrapper(logger hclog.Logger,
	blockchain *blockchain.Blockchain,
	executor *state.Executor) *BlockchainWrapper {
	return &BlockchainWrapper{
		logger:     logger,
		executor:   executor,
		blockchain: blockchain,
	}
}

// CurrentHeader returns the header of blockchain block head
func (p *BlockchainWrapper) CurrentHeader() *types.Header {
	return p.blockchain.Header()
}

// CommitBlock commits a block to the chain
func (p *BlockchainWrapper) CommitBlock(block *types.FullBlock) error {
	return p.blockchain.WriteFullBlock(block, consensusSource)
}

// ProcessBlock builds a final block from given 'block' on top of 'parent'
func (p *BlockchainWrapper) ProcessBlock(parent *types.Header, block *types.Block) (*types.FullBlock, error) {
	header := block.Header.Copy()
	start := time.Now().UTC()

	transition, err := p.executor.ProcessBlock(parent.StateRoot, block, types.Address(header.Miner))
	if err != nil {
		return nil, fmt.Errorf("failed to process block: %w", err)
	}

	_, root, err := transition.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit the state changes: %w", err)
	}

	metrics.UpdateBlockExecutionMetric(start)

	if root != block.Header.StateRoot {
		return nil, fmt.Errorf("incorrect state root: (%s, %s)", root, block.Header.StateRoot)
	}

	// build the block
	builtBlock := consensus.BuildBlock(consensus.BuildBlockParams{
		Header:   header,
		Txns:     block.Transactions,
		Receipts: transition.Receipts(),
	})

	if builtBlock.Header.TxRoot != block.Header.TxRoot {
		return nil, fmt.Errorf("incorrect tx root (expected: %s, actual: %s)",
			builtBlock.Header.TxRoot, block.Header.TxRoot)
	}

	return &types.FullBlock{
		Block:    builtBlock,
		Receipts: transition.Receipts(),
	}, nil
}

// GetStateProviderForBlock is an implementation of blockchainBackend interface
func (p *BlockchainWrapper) GetStateProviderForBlock(header *types.Header) (contract.Provider, error) {
	transition, err := p.executor.BeginTxn(header.StateRoot, header, types.ZeroAddress)
	if err != nil {
		return nil, err
	}

	return systemstate.NewStateProvider(transition), nil
}

// GetStateProvider returns a reference to make queries to the provided state
func (p *BlockchainWrapper) GetStateProvider(transition *state.Transition) contract.Provider {
	return systemstate.NewStateProvider(transition)
}

// GetHeaderByNumber is an implementation of blockchainBackend interface
func (p *BlockchainWrapper) GetHeaderByNumber(number uint64) (*types.Header, bool) {
	return p.blockchain.GetHeaderByNumber(number)
}

// GetHeaderByHash is an implementation of blockchainBackend interface
func (p *BlockchainWrapper) GetHeaderByHash(hash types.Hash) (*types.Header, bool) {
	return p.blockchain.GetHeaderByHash(hash)
}

// NewBlockBuilder is an implementation of blockchainBackend interface
func (p *BlockchainWrapper) NewBlockBuilder(
	parent *types.Header, coinbase types.Address,
	txPool TxPool, blockTime time.Duration, logger hclog.Logger) (BlockBuilder, error) {
	gasLimit, err := p.blockchain.CalculateGasLimit(parent.Number + 1)
	if err != nil {
		return nil, err
	}

	return NewBlockBuilder(&BlockBuilderParams{
		BlockTime: blockTime,
		Parent:    parent,
		Coinbase:  coinbase,
		Executor:  p.executor,
		GasLimit:  gasLimit,
		BaseFee:   p.blockchain.CalculateBaseFee(parent),
		TxPool:    txPool,
		Logger:    logger,
	}), nil
}

// GetSystemState is an implementation of blockchainBackend interface
func (p *BlockchainWrapper) GetSystemState(provider contract.Provider) systemstate.SystemState {
	return systemstate.NewSystemState(contracts.EpochManagerContract, contracts.BridgeStorageContract, provider)
}

func (p *BlockchainWrapper) SubscribeEvents() blockchain.Subscription {
	return p.blockchain.SubscribeEvents()
}

func (p *BlockchainWrapper) UnubscribeEvents(subscription blockchain.Subscription) {
	p.blockchain.UnsubscribeEvents(subscription)
}

func (p *BlockchainWrapper) GetChainID() uint64 {
	return uint64(p.blockchain.Config().ChainID) //nolint:gosec
}

func (p *BlockchainWrapper) GetReceiptsByHash(hash types.Hash) ([]*types.Receipt, error) {
	return p.blockchain.GetReceiptsByHash(hash)
}
