package polybft

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

var _ Bridge = (*bridge)(nil)

// bridge is a struct that manages different bridges
type bridge struct {
	bridgeManagers  map[uint64]BridgeManager
	state           *State
	internalChainID uint64
	relayer         BridgeEventRelayer
}

// Bridge is an interface that defines functions that a bridge must implement
type Bridge interface {
	Close()
	PostBlock(req *PostBlockRequest) error
	PostEpoch(req *PostEpochRequest) error
	BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error)
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
	internalChainID := runtimeConfig.blockchain.GetChainID()

	bridge := &bridge{
		bridgeManagers:  make(map[uint64]BridgeManager),
		state:           runtimeConfig.State,
		internalChainID: internalChainID,
	}

	for externalChainID, cfg := range runtimeConfig.GenesisConfig.Bridge {
		bridgeManager := newBridgeManager(logger, runtimeConfig.State, &bridgeEventManagerConfig{
			bridgeCfg:         cfg,
			topic:             runtimeConfig.bridgeTopic,
			key:               runtimeConfig.Key,
			maxNumberOfEvents: maxNumberOfEvents,
		}, runtime, externalChainID, internalChainID)
		bridge.bridgeManagers[externalChainID] = bridgeManager

		if err := bridgeManager.Start(runtimeConfig); err != nil {
			return nil, fmt.Errorf("error starting bridge manager for chainID: %d, err: %w", externalChainID, err)
		}
	}

	relayer, err := newBridgeEventRelayer(runtimeConfig, logger)
	if err != nil {
		return nil, err
	}

	bridge.relayer = relayer

	if err := relayer.Start(runtimeConfig, eventProvider); err != nil {
		return nil, fmt.Errorf("error starting bridge event relayer, err: %w", err)
	}

	return bridge, nil
}

// Close calls Close on each bridge manager, which stops ongoing go routines in manager
func (b *bridge) Close() {
	for _, bridgeManager := range b.bridgeManagers {
		bridgeManager.Close()
	}

	b.relayer.Close()
}

// PostBlock is a function executed on every block finalization (either by consensus or syncer)
// and calls PostBlock in each bridge manager
func (b bridge) PostBlock(req *PostBlockRequest) error {
	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostBlock(); err != nil {
			return fmt.Errorf("erorr bridge post block, chainID: %d, err: %w", chainID, err)
		}
	}

	if err := b.relayer.PostBlock(req); err != nil {
		return err
	}

	return nil
}

// PostEpoch is a function executed on epoch ending / start of new epoch
// and calls PostEpoch in each bridge manager
func (b *bridge) PostEpoch(req *PostEpochRequest) error {
	if err := b.state.EpochStore.insertEpoch(req.NewEpochID, req.DBTx, b.internalChainID); err != nil {
		return fmt.Errorf("error inserting epoch to internal, err: %w", err)
	}

	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostEpoch(req); err != nil {
			return fmt.Errorf("erorr bridge post epoch, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// BridgeBatch returns the pending signed bridge batches as a list of signed bridge batches
func (b *bridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	bridgeBatches := make([]*BridgeBatchSigned, 0, len(b.bridgeManagers))

	for chainID, bridgeManager := range b.bridgeManagers {
		bridgeBatch, err := bridgeManager.BridgeBatch(pendingBlockNumber)
		if err != nil {
			return nil, fmt.Errorf("error while getting signed batches for chainID: %d, err: %w", chainID, err)
		}

		bridgeBatches = append(bridgeBatches, bridgeBatch)
	}

	return bridgeBatches, nil
}
