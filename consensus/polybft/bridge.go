package polybft

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

var _ Bridge = (*bridge)(nil)

// bridge is a struct that manages different bridges
type bridge map[uint64]BridgeManager

// Bridge is an interface that defines functions that a bridge must implement
type Bridge interface {
	Close()
	PostBlock(req *PostBlockRequest) error
	PostEpoch(req *PostEpochRequest) error
	BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error)
	InsertEpoch(epoch uint64, tx *bolt.Tx) error
}

var _ Bridge = (*dummyBridge)(nil)

type dummyBridge map[uint64]BridgeManager

func (d *dummyBridge) Close()                                {}
func (d *dummyBridge) PostBlock(req *PostBlockRequest) error { return nil }
func (d *dummyBridge) PostEpoch(req *PostEpochRequest) error { return nil }
func (d *dummyBridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *dummyBridge) InsertEpoch(epoch uint64, tx *bolt.Tx) error { return nil }

// newBridge creates a new instance of bridge
func newBridge(runtime Runtime,
	runtimeConfig *runtimeConfig,
	eventProvider *EventProvider,
	logger hclog.Logger) (Bridge, error) {
	bridge := make(bridge)

	for chainID := range runtimeConfig.GenesisConfig.Bridge {
		bridgeManager, err := newBridgeManager(runtime, runtimeConfig, eventProvider, logger, chainID)
		if err != nil {
			return nil, err
		}

		bridge[chainID] = bridgeManager
	}

	return bridge, nil
}

// Close calls Close on each bridge manager, which stops ongoing go routines in manager
func (b bridge) Close() {
	for _, bridgeManager := range b {
		bridgeManager.Close()
	}
}

// PostBlock is a function executed on every block finalization (either by consensus or syncer)
// and calls PostBlock in each bridge manager
func (b bridge) PostBlock(req *PostBlockRequest) error {
	for chainID, bridgeManager := range b {
		if err := bridgeManager.PostBlock(req); err != nil {
			return fmt.Errorf("erorr bridge post block, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// PostEpoch is a function executed on epoch ending / start of new epoch
// and calls PostEpoch in each bridge manager
func (b bridge) PostEpoch(req *PostEpochRequest) error {
	isInserted := false

	for chainID, bridgeManager := range b {
		if err := bridgeManager.PostEpoch(req); err != nil {
			return fmt.Errorf("erorr bridge post epoch, chainID: %d, err: %w", chainID, err)
		}

		if !isInserted {
			if err := bridgeManager.InsertEpochInternal(req.NewEpochID, req.DBTx); err != nil {
				return err
			} else {
				isInserted = true
			}
		}
	}

	return nil
}

// BridgeBatch returns the pending signed bridge batches as a list of signed bridge batches
func (b bridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	bridgeBatches := make([]*BridgeBatchSigned, 0, len(b))

	for chainID, bridgeManager := range b {
		bridgeBatch, err := bridgeManager.BridgeBatch(pendingBlockNumber)
		if err != nil {
			return nil, fmt.Errorf("error while getting signed batches for chainID: %d, err: %w", chainID, err)
		}

		bridgeBatches = append(bridgeBatches, bridgeBatch)
	}

	return bridgeBatches, nil
}

// InsertEpoch calls InsertEpoch in each bridge manager on chain
func (b bridge) InsertEpoch(epoch uint64, dbTx *bolt.Tx) error {
	for _, brigeManager := range b {
		if err := brigeManager.InsertEpochExternal(epoch, dbTx); err != nil {
			return err
		}
	}

	return nil
}
