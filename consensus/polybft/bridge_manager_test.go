package polybft

import (
	"math/big"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	bolt "go.etcd.io/bbolt"
)

type mockBridgeManager struct {
	chainID uint64
	state   *State
}

func (mbm *mockBridgeManager) Close()                                        {}
func (mbm *mockBridgeManager) AddLog(chainID *big.Int, log *ethgo.Log) error { return nil }
func (mbm *mockBridgeManager) PostBlock(req *PostBlockRequest) error         { return nil }
func (mbm *mockBridgeManager) PostEpoch(req *PostEpochRequest) error         { return nil }
func (mbm *mockBridgeManager) BuildExitEventRoot(epoch uint64) (types.Hash, error) {
	return types.ZeroHash, nil
}
func (mbm *mockBridgeManager) BridgeBatch(pendingBlockNumber uint64) (*BridgeBatchSigned, error) {
	return nil, nil
}
func (mbm *mockBridgeManager) InsertEpoch(epoch uint64, dbTx *bolt.Tx) error {
	if err := mbm.state.EpochStore.insertEpoch(epoch, dbTx, mbm.chainID); err != nil {
		return err
	}

	return nil
}
