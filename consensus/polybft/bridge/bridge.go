package bridge

import (
	"fmt"

	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"
)

// topic is an interface for p2p message gossiping
type Topic interface {
	Publish(obj proto.Message) error
	Subscribe(handler func(obj interface{}, from peer.ID)) error
}

var _ Bridge = (*bridge)(nil)

// bridge is a struct that manages different bridges
type bridge struct {
	bridgeManagers  map[uint64]BridgeManager
	state           *BridgeManagerStore
	internalChainID uint64
	relayer         BridgeEventRelayer
	logger          hclog.Logger
}

// Bridge is an interface that defines functions that a bridge must implement
type Bridge interface {
	Close()
	PostBlock(req *polytypes.PostBlockRequest) error
	PostEpoch(req *polytypes.PostEpochRequest) error
	BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error)
}

var _ Bridge = (*DummyBridge)(nil)

type DummyBridge struct{}

func (d *DummyBridge) Close()                                          {}
func (d *DummyBridge) PostBlock(req *polytypes.PostBlockRequest) error { return nil }
func (d *DummyBridge) PostEpoch(req *polytypes.PostEpochRequest) error { return nil }
func (d *DummyBridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *DummyBridge) InsertEpoch(epoch uint64, tx *bolt.Tx) error { return nil }

// newBridge creates a new instance of bridge
func NewBridge(runtime Runtime,
	state *state.State,
	runtimeConfig *config.Runtime,
	bridgeTopic Topic,
	eventProvider *state.EventProvider,
	blockchain polychain.Blockchain,
	logger hclog.Logger,
	dbTx *bolt.Tx) (Bridge, error) {
	if len(runtimeConfig.GenesisConfig.Bridge) == 0 {
		return &DummyBridge{}, nil
	}

	internalChainID := blockchain.GetChainID()
	chainIDs := make([]uint64, 0, len(runtimeConfig.GenesisConfig.Bridge)+1)
	chainIDs = append(chainIDs, internalChainID)

	for chainID := range runtimeConfig.GenesisConfig.Bridge {
		chainIDs = append(chainIDs, chainID)
	}

	store, err := newBridgeManagerStore(state.DB(), dbTx, chainIDs)
	if err != nil {
		return nil, fmt.Errorf("error creating bridge manager store, err: %w", err)
	}

	bridge := &bridge{
		bridgeManagers:  make(map[uint64]BridgeManager),
		state:           store,
		internalChainID: internalChainID,
		logger:          logger,
	}

	for externalChainID, cfg := range runtimeConfig.GenesisConfig.Bridge {
		bridgeManager := newBridgeManager(logger, store, &bridgeEventManagerConfig{
			bridgeCfg:         cfg,
			topic:             bridgeTopic,
			key:               runtimeConfig.Key,
			maxNumberOfEvents: maxNumberOfBatchEvents,
		}, runtime, externalChainID, internalChainID)
		bridge.bridgeManagers[externalChainID] = bridgeManager

		if err := bridgeManager.Start(runtimeConfig); err != nil {
			return nil, fmt.Errorf("error starting bridge manager for chainID: %d, err: %w", externalChainID, err)
		}
	}

	relayer, err := newBridgeEventRelayer(blockchain, runtimeConfig, logger)
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
func (b bridge) PostBlock(req *polytypes.PostBlockRequest) error {
	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostBlock(); err != nil {
			return fmt.Errorf("erorr bridge post block, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// PostEpoch is a function executed on epoch ending / start of new epoch
// and calls PostEpoch in each bridge manager
func (b *bridge) PostEpoch(req *polytypes.PostEpochRequest) error {
	if err := b.state.cleanEpochsFromDB(req.DBTx); err != nil {
		// we just log this, as it is not critical
		b.logger.Error("error cleaning epochs from db", "err", err)
	}

	if err := b.state.insertEpoch(req.NewEpochID, req.DBTx, b.internalChainID); err != nil {
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
