package polybft

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

var _ Bridge = (*bridge)(nil)

// bridge is a struct that manages different bridges
type bridge struct {
	bridgeManagers     map[uint64]BridgeManager
	state              *State
	internalChainID    uint64
	bridgeEventRelayer BridgeEventRelayer
}

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
	internalChainID := runtimeConfig.blockchain.GetChainID()

	bridge := &bridge{
		bridgeManagers:  make(map[uint64]BridgeManager),
		state:           runtimeConfig.State,
		internalChainID: internalChainID,
	}

	for externalChainID := range runtimeConfig.GenesisConfig.Bridge {
		bridgeManager, err := newBridgeManager(runtime, runtimeConfig, eventProvider,
			logger, externalChainID, internalChainID)
		if err != nil {
			return nil, err
		}

		bridge.bridgeManagers[externalChainID] = bridgeManager
	}

	return bridge, nil
}

// initStateSyncRelayer initializes bridge event relayer
// if not enabled, then a dummy bridge event relayer will be used
func (b *bridge) initBridgeEventRelayer(
	eventProvider *EventProvider,
	runtimeConfig *runtimeConfig,
	logger hclog.Logger) error {
	if runtimeConfig.consensusConfig.IsRelayer {
		txRelayerMap := make(map[uint64]txrelayer.TxRelayer)
		eventTrackerConfigs := make([]*eventTrackerConfig, 0, len(runtimeConfig.GenesisConfig.Bridge))
		for chainID, config := range runtimeConfig.GenesisConfig.Bridge {
			txRelayer, err := getBridgeTxRelayer(config.JSONRPCEndpoint, logger)
			if err != nil {
				return err
			}

			txRelayerMap[chainID] = txRelayer
			eventTrackerConfigs = append(eventTrackerConfigs, &eventTrackerConfig{
				gatewayAddr:         config.ExternalGatewayAddr,
				jsonrpcAddr:         config.JSONRPCEndpoint,
				startBlock:          config.EventTrackerStartBlocks[config.ExternalGatewayAddr],
				trackerPollInterval: runtimeConfig.GenesisConfig.BlockTrackerPollInterval.Duration,
			})
		}

		bridgeEventRelayer := newBridgeEventRelayer(
			txRelayerMap,
			runtimeConfig,
			wallet.NewEcdsaSigner(runtimeConfig.Key),
			logger.Named("bridge_event_relayer"),
			eventTrackerConfigs,
		)

		bridgeEventRelayer.initTrackers(runtimeConfig)

		b.bridgeEventRelayer = bridgeEventRelayer

	} else {
		b.bridgeEventRelayer = &dummyBridgeEventRelayer{}
	}

	eventProvider.Subscribe(b.bridgeEventRelayer)

	return nil
}

// Close calls Close on each bridge manager, which stops ongoing go routines in manager
func (b *bridge) Close() {
	for _, bridgeManager := range b.bridgeManagers {
		bridgeManager.Close()
	}
}

// PostBlock is a function executed on every block finalization (either by consensus or syncer)
// and calls PostBlock in each bridge manager
func (b bridge) PostBlock(req *PostBlockRequest) error {
	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostBlock(req); err != nil {
			return fmt.Errorf("erorr bridge post block, chainID: %d, err: %w", chainID, err)
		}
	}

	if err := b.bridgeEventRelayer.PostBlock(req); err != nil {
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

	if err := b.InsertEpoch(req.NewEpochID, req.DBTx); err != nil {
		return fmt.Errorf("error inserting epoch to external, err: %w", err)
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

// InsertEpoch calls InsertEpoch in each bridge manager on chain
func (b *bridge) InsertEpoch(epoch uint64, dbTx *bolt.Tx) error {
	for _, brigeManager := range b.bridgeManagers {
		if err := brigeManager.InsertEpoch(epoch, dbTx); err != nil {
			return err
		}
	}

	return nil
}
