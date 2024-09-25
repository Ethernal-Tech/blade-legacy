package blockchain

import (
	"time"

	"github.com/0xPolygon/polygon-edge/blockchain"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	"github.com/0xPolygon/polygon-edge/helper/progress"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/syncer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo/contract"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/mock"
)

var _ Blockchain = (*BlockchainMock)(nil)

type BlockchainMock struct {
	mock.Mock
}

func (m *BlockchainMock) CurrentHeader() *types.Header {
	args := m.Called()

	return args.Get(0).(*types.Header)
}

func (m *BlockchainMock) CommitBlock(block *types.FullBlock) error {
	args := m.Called(block)

	return args.Error(0)
}

func (m *BlockchainMock) NewBlockBuilder(parent *types.Header, coinbase types.Address,
	txPool TxPool, blockTime time.Duration, logger hclog.Logger) (BlockBuilder, error) {
	args := m.Called()

	return args.Get(0).(BlockBuilder), args.Error(1)
}

func (m *BlockchainMock) ProcessBlock(parent *types.Header, block *types.Block) (*types.FullBlock, error) {
	args := m.Called(parent, block)

	return args.Get(0).(*types.FullBlock), args.Error(1)
}

func (m *BlockchainMock) GetStateProviderForBlock(block *types.Header) (contract.Provider, error) {
	args := m.Called(block)
	stateProvider, _ := args.Get(0).(contract.Provider)

	return stateProvider, nil
}

func (m *BlockchainMock) GetStateProvider(transition *state.Transition) contract.Provider {
	args := m.Called()
	stateProvider, _ := args.Get(0).(contract.Provider)

	return stateProvider
}

func (m *BlockchainMock) GetHeaderByNumber(number uint64) (*types.Header, bool) {
	args := m.Called(number)

	if len(args) == 1 {
		header, ok := args.Get(0).(*types.Header)

		if ok {
			return header, true
		}

		getHeaderCallback, ok := args.Get(0).(func(number uint64) *types.Header)
		if ok {
			h := getHeaderCallback(number)

			return h, h != nil
		}
	} else if len(args) == 2 {
		return args.Get(0).(*types.Header), args.Get(1).(bool)
	}

	panic("Unsupported mock for GetHeaderByNumber") //nolint:gocritic
}

func (m *BlockchainMock) GetHeaderByHash(hash types.Hash) (*types.Header, bool) {
	args := m.Called(hash)
	header, ok := args.Get(0).(*types.Header)

	if ok {
		return header, true
	}

	getHeaderCallback, ok := args.Get(0).(func(hash types.Hash) *types.Header)
	if ok {
		h := getHeaderCallback(hash)

		return h, h != nil
	}

	panic("Unsupported mock for GetHeaderByHash") //nolint:gocritic
}

func (m *BlockchainMock) GetSystemState(provider contract.Provider) systemstate.SystemState {
	args := m.Called(provider)

	return args.Get(0).(systemstate.SystemState)
}

func (m *BlockchainMock) SubscribeEvents() blockchain.Subscription {
	return nil
}

func (m *BlockchainMock) UnubscribeEvents(blockchain.Subscription) {
}

func (m *BlockchainMock) CalculateGasLimit(number uint64) (uint64, error) {
	return 0, nil
}

func (m *BlockchainMock) GetChainID() uint64 {
	return 0
}

func (m *BlockchainMock) GetReceiptsByHash(hash types.Hash) ([]*types.Receipt, error) {
	args := m.Called(hash)

	return args.Get(0).([]*types.Receipt), args.Error(1)
}

var _ BlockBuilder = (*BlockBuilderMock)(nil)

type BlockBuilderMock struct {
	mock.Mock
}

func (m *BlockBuilderMock) Reset() error {
	args := m.Called()
	if len(args) == 0 {
		return nil
	}

	return args.Error(0)
}

func (m *BlockBuilderMock) WriteTx(tx *types.Transaction) error {
	args := m.Called(tx)
	if len(args) == 0 {
		return nil
	}

	return args.Error(0)
}

func (m *BlockBuilderMock) Fill() {
	m.Called()
}

// Receipts returns the collection of transaction receipts for given block
func (m *BlockBuilderMock) Receipts() []*types.Receipt {
	args := m.Called()

	return args.Get(0).([]*types.Receipt)
}

func (m *BlockBuilderMock) Build(handler func(*types.Header)) (*types.FullBlock, error) {
	args := m.Called(handler)
	builtBlock := args.Get(0).(*types.FullBlock)

	handler(builtBlock.Block.Header)

	return builtBlock, nil
}

func (m *BlockBuilderMock) GetState() *state.Transition {
	args := m.Called()

	return args.Get(0).(*state.Transition)
}

var _ TxPool = (*TxPoolMock)(nil)

type TxPoolMock struct {
	mock.Mock
}

func (tp *TxPoolMock) Prepare() {
	tp.Called()
}

func (tp *TxPoolMock) Length() uint64 {
	args := tp.Called()

	return args[0].(uint64)
}

func (tp *TxPoolMock) Peek() *types.Transaction {
	args := tp.Called()

	return args[0].(*types.Transaction)
}

func (tp *TxPoolMock) Pop(tx *types.Transaction) {
	tp.Called(tx)
}

func (tp *TxPoolMock) Drop(tx *types.Transaction) {
	tp.Called(tx)
}

func (tp *TxPoolMock) Demote(tx *types.Transaction) {
	tp.Called(tx)
}

func (tp *TxPoolMock) SetSealing(v bool) {
	tp.Called(v)
}

func (tp *TxPoolMock) ResetWithBlock(fullBlock *types.Block) {
	tp.Called(fullBlock)
}

func (tp *TxPoolMock) ReinsertProposed() {
	tp.Called()
}

func (tp *TxPoolMock) ClearProposed() {
	tp.Called()
}

var _ syncer.Syncer = (*SyncerMock)(nil)

type SyncerMock struct {
	mock.Mock
}

func (tp *SyncerMock) Start() error {
	args := tp.Called()

	return args.Error(0)
}

func (tp *SyncerMock) Close() error {
	args := tp.Called()

	return args.Error(0)
}

func (tp *SyncerMock) GetSyncProgression() *progress.Progression {
	args := tp.Called()

	return args[0].(*progress.Progression)
}

func (tp *SyncerMock) HasSyncPeer() bool {
	args := tp.Called()

	return args[0].(bool)
}

func (tp *SyncerMock) Sync(func(*types.FullBlock) bool) error {
	args := tp.Called()

	return args.Error(0)
}

func (tp *SyncerMock) UpdateBlockTimeout(time.Duration) {
	tp.Called()
}
