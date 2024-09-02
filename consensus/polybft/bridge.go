package polybft

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

var _ Bridge = (*bridge)(nil)

// bridge is a struct that manages different bridges
type bridge struct {
	bridgeManagers map[uint64]BridgeManager
}

// Bridge is an interface that defines function that a bridge must implement
type Bridge interface {
	Close()
	PostBlockAsync(req *PostBlockRequest)
	PostBlock(req *PostBlockRequest) error
	PostEpoch(req *PostEpochRequest) error
	BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error)
	InsertEpoch(epoch uint64, tx *bolt.Tx) error
}

var _ Bridge = (*dummyBridge)(nil)

type dummyBridge struct{}

func (d *dummyBridge) Close()                                {}
func (d *dummyBridge) PostBlockAsync(req *PostBlockRequest)  {}
func (d *dummyBridge) PostBlock(req *PostBlockRequest) error { return nil }
func (d *dummyBridge) PostEpoch(req *PostEpochRequest) error { return nil }
func (d *dummyBridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *dummyBridge) InsertEpoch(epoch uint64, tx *bolt.Tx) error { return nil }

// newBridge creates a new instance of bridge
func newBridge(bridgeBackend BridgeBackend,
	runtimeConfig *runtimeConfig,
	eventProvider *EventProvider,
	logger hclog.Logger) (Bridge, error) {
	bridgeManagers := make(map[uint64]BridgeManager)

	for chainID := range runtimeConfig.GenesisConfig.Bridge {
		bridgeManager, err := newBridgeManager(bridgeBackend, runtimeConfig, eventProvider, logger, chainID)
		if err != nil {
			return nil, err
		}

		bridgeManagers[chainID] = bridgeManager
	}

	return &bridge{
		bridgeManagers: bridgeManagers,
	}, nil
}

// Close calls bridge manager close which stops ongoing go routines in manager
func (b *bridge) Close() {
	for _, bridgeManager := range b.bridgeManagers {
		bridgeManager.Close()
	}
}

// PostBlockAsync is called on finalization of each block (either from consensus or synncer)
// but it doesn't require return of any kind and is done asynchronously
func (b *bridge) PostBlockAsync(req *PostBlockRequest) {
	for _, bridgeManager := range b.bridgeManagers {
		bridgeManager.PostBlockAsync(req)
	}
}

// PostBlock is a function executed on every block finalization (either by consensus or syncer)
// and calls PostBlock in each bridge manager
func (b *bridge) PostBlock(req *PostBlockRequest) error {
	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostBlock(req); err != nil {
			return fmt.Errorf("erorr bridge post block, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// PostEpoch is a function executed on epoch ending / start of new epoch
// and calls PostEpoch in each bridge manager
func (b *bridge) PostEpoch(req *PostEpochRequest) error {
	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostEpoch(req); err != nil {
			return fmt.Errorf("erorr bridge post epoch, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// BridgeBatch returns the pending signed bridge batches as a list of signed bridge batches
func (b *bridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	var bridgeBatches []*BridgeBatchSigned

	for chainID, bridgeManager := range b.bridgeManagers {
		bridgeBatch, err := bridgeManager.BridgeBatch(pendingBlockNumber)
		if err != nil {
			return nil, fmt.Errorf("error while getting signed batches chainID: %d, err: %w", chainID, err)
		}

		bridgeBatches = append(bridgeBatches, bridgeBatch)
	}

	return bridgeBatches, nil
}

// InsertEpoch cals InsertEpoch in each bridge manager on chain
func (b *bridge) InsertEpoch(epoch uint64, dbTx *bolt.Tx) error {
	for _, brigeManager := range b.bridgeManagers {
		if err := brigeManager.InsertEpoch(epoch, dbTx); err != nil {
			return err
		}
	}

	return nil
}
