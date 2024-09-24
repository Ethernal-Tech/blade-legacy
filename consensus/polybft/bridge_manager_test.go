package polybft

import (
	"math/big"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	bolt "go.etcd.io/bbolt"
)

var _ BridgeManager = (*mockBridgeManager)(nil)

type mockBridgeManager struct {
	chainID uint64
	state   *State
}

func (*mockBridgeManager) AddLog(chainID *big.Int, eventLog *ethgo.Log) error {
	return nil
}

func (*mockBridgeManager) Close() {}

func (*mockBridgeManager) GetLogFilters() map[types.Address][]types.Hash {
	return nil
}

// PostBlock implements BridgeManager.
func (*mockBridgeManager) PostBlock() error {
	return nil
}

// PostEpoch implements BridgeManager.
func (mbm *mockBridgeManager) PostEpoch(req *PostEpochRequest) error {
	if err := mbm.state.EpochStore.insertEpoch(req.NewEpochID, req.DBTx, mbm.chainID); err != nil {
		return err
	}

	return nil
}

// ProcessLog implements BridgeManager.
func (*mockBridgeManager) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	return nil
}

// Start implements BridgeManager.
func (*mockBridgeManager) Start(runtimeCfg *runtimeConfig) error {
	return nil
}

func (mbm *mockBridgeManager) BuildExitEventRoot(epoch uint64) (types.Hash, error) {
	return types.ZeroHash, nil
}
func (mbm *mockBridgeManager) BridgeBatch(pendingBlockNumber uint64) (*BridgeBatchSigned, error) {
	return nil, nil
}
