package polybft

import (
	"fmt"
	"path"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/blockchain-event-tracker/store"
	"github.com/Ethernal-Tech/blockchain-event-tracker/tracker"
	hcf "github.com/hashicorp/go-hclog"
	"github.com/umbracle/ethgo"
)

var (
	stateSyncEventSig           = new(contractsapi.StateSyncedEvent).Sig()
	checkpointSubmittedEventSig = new(contractsapi.CheckpointSubmittedEvent).Sig()
)

// BridgeBackend is an interface that defines functions required by bridge components
type BridgeBackend interface {
	Runtime
	StateSyncProofRetriever
	EventProofRetriever
}

// eventTrackerConfig is a struct that holds the event tracker configuration
type eventTrackerConfig struct {
	consensus.EventTracker

	jsonrpcAddr           string
	startBlock            uint64
	stateSenderAddr       types.Address
	checkpointManagerAddr types.Address
	trackerPollInterval   time.Duration
}

// bridgeManager is a struct that manages different bridge components
// such as handling and executing bridge events
type bridgeManager struct {
	checkpointManager CheckpointManager
	stateSyncManager  StateSyncManager
	stateSyncRelayer  StateSyncRelayer
	exitEventRelayer  ExitRelayer

	eventTrackerConfig *eventTrackerConfig
	logger             hcf.Logger

	isBridgeEnabled bool
}

// newBridgeManager creates a new instance of bridgeManager
func newBridgeManager(
	bridgeBackend BridgeBackend,
	runtimeConfig *runtimeConfig,
	eventProvider *EventProvider,
	logger hcf.Logger) (*bridgeManager, error) {
	stateSenderAddr := runtimeConfig.GenesisConfig.Bridge.StateSenderAddr
	bridgeManager := &bridgeManager{
		logger:          logger.Named("bridge-manager"),
		isBridgeEnabled: runtimeConfig.GenesisConfig.IsBridgeEnabled(),
		eventTrackerConfig: &eventTrackerConfig{
			EventTracker:          *runtimeConfig.eventTracker,
			stateSenderAddr:       stateSenderAddr,
			checkpointManagerAddr: runtimeConfig.GenesisConfig.Bridge.CheckpointManagerAddr,
			jsonrpcAddr:           runtimeConfig.GenesisConfig.Bridge.JSONRPCEndpoint,
			startBlock:            runtimeConfig.GenesisConfig.Bridge.EventTrackerStartBlocks[stateSenderAddr],
			trackerPollInterval:   runtimeConfig.GenesisConfig.BlockTrackerPollInterval.Duration,
		},
	}

	if err := bridgeManager.initStateSyncManager(bridgeBackend, runtimeConfig, logger); err != nil {
		return nil, err
	}

	if err := bridgeManager.initCheckpointManager(eventProvider, runtimeConfig, logger); err != nil {
		return nil, err
	}

	if err := bridgeManager.initStateSyncRelayer(bridgeBackend, eventProvider, runtimeConfig, logger); err != nil {
		return nil, err
	}

	if err := bridgeManager.initExitRelayer(bridgeBackend, runtimeConfig, logger); err != nil {
		return nil, err
	}

	if bridgeManager.isBridgeEnabled {
		if err := bridgeManager.initTracker(runtimeConfig); err != nil {
			return nil, fmt.Errorf("failed to init event tracker. Error: %w", err)
		}
	}

	return bridgeManager, nil
}

// PostBlock is a function executed on every block finalization (either by consensus or syncer)
func (b *bridgeManager) PostBlock(req *PostBlockRequest) error {
	if err := b.stateSyncManager.PostBlock(req); err != nil {
		return fmt.Errorf("failed to execute post block in state sync manager. Err: %w", err)
	}

	if err := b.stateSyncRelayer.PostBlock(req); err != nil {
		return fmt.Errorf("failed to execute post block in state sync relayer. Err: %w", err)
	}

	if err := b.exitEventRelayer.PostBlock(req); err != nil {
		return fmt.Errorf("failed to execute post block in exit event relayer. Err: %w", err)
	}

	return nil
}

// close stops ongoing go routines in the manager
func (b *bridgeManager) close() {
	b.stateSyncRelayer.Close()
}

// initStateSyncManager initializes state sync manager
// if bridge is not enabled, then a dummy state sync manager will be used
func (b *bridgeManager) initStateSyncManager(
	bridgeBackend BridgeBackend,
	runtimeConfig *runtimeConfig,
	logger hcf.Logger) error {
	if b.isBridgeEnabled {
		stateSyncManager := newStateSyncManager(
			logger.Named("state-sync-manager"),
			runtimeConfig.State,
			&stateSyncConfig{
				key:               runtimeConfig.Key,
				dataDir:           runtimeConfig.DataDir,
				topic:             runtimeConfig.bridgeTopic,
				maxCommitmentSize: maxCommitmentSize,
			},
			bridgeBackend,
		)

		b.stateSyncManager = stateSyncManager
	} else {
		b.stateSyncManager = &dummyStateSyncManager{}
	}

	return b.stateSyncManager.Init()
}

// initCheckpointManager initializes checkpoint manager
// if bridge is not enabled, then a dummy checkpoint manager will be used
func (b *bridgeManager) initCheckpointManager(
	eventProvider *EventProvider,
	runtimeConfig *runtimeConfig,
	logger hcf.Logger) error {
	if b.isBridgeEnabled {
		// enable checkpoint manager
		txRelayer, err := txrelayer.NewTxRelayer(
			txrelayer.WithIPAddress(runtimeConfig.GenesisConfig.Bridge.JSONRPCEndpoint),
			txrelayer.WithWriter(logger.StandardWriter(&hcf.StandardLoggerOptions{})))
		if err != nil {
			return err
		}

		b.checkpointManager = newCheckpointManager(
			wallet.NewEcdsaSigner(runtimeConfig.Key),
			runtimeConfig.GenesisConfig.Bridge.CheckpointManagerAddr,
			txRelayer,
			runtimeConfig.blockchain,
			runtimeConfig.polybftBackend,
			logger.Named("checkpoint_manager"),
			runtimeConfig.State)
	} else {
		b.checkpointManager = &dummyCheckpointManager{}
	}

	eventProvider.Subscribe(b.checkpointManager)

	return nil
}

// initStateSyncRelayer initializes state sync relayer
// if not enabled, then a dummy state sync relayer will be used
func (b *bridgeManager) initStateSyncRelayer(
	bridgeBackend BridgeBackend,
	eventProvider *EventProvider,
	runtimeConfig *runtimeConfig,
	logger hcf.Logger) error {
	if b.isBridgeEnabled && runtimeConfig.consensusConfig.IsRelayer {
		txRelayer, err := getBridgeTxRelayer(runtimeConfig.consensusConfig.RPCEndpoint, logger)
		if err != nil {
			return err
		}

		b.stateSyncRelayer = newStateSyncRelayer(
			txRelayer,
			contracts.StateReceiverContract,
			runtimeConfig.State.StateSyncStore,
			bridgeBackend,
			runtimeConfig.blockchain,
			wallet.NewEcdsaSigner(runtimeConfig.Key),
			nil,
			logger.Named("state_sync_relayer"))
	} else {
		b.stateSyncRelayer = &dummyStateSyncRelayer{}
	}

	eventProvider.Subscribe(b.stateSyncRelayer)

	return b.stateSyncRelayer.Init()
}

// initStateSyncRelayer initializes exit event relayer
// if not enabled, then a dummy exit event relayer will be used
func (b *bridgeManager) initExitRelayer(
	bridgeBackend BridgeBackend,
	runtimeConfig *runtimeConfig,
	logger hcf.Logger) error {
	if b.isBridgeEnabled && runtimeConfig.consensusConfig.IsRelayer {
		txRelayer, err := getBridgeTxRelayer(runtimeConfig.consensusConfig.RPCEndpoint, logger)
		if err != nil {
			return err
		}

		b.exitEventRelayer = newExitRelayer(
			txRelayer,
			wallet.NewEcdsaSigner(runtimeConfig.Key),
			bridgeBackend,
			runtimeConfig.blockchain,
			runtimeConfig.State.CheckpointStore,
			logger.Named("state_sync_relayer"))
	} else {
		b.exitEventRelayer = &dummyExitRelayer{}
	}

	return nil
}

// initTracker starts a new event tracker (to receive new state sync events)
func (b *bridgeManager) initTracker(
	runtimeConfig *runtimeConfig) error {
	store, err := store.NewBoltDBEventTrackerStore(path.Join(runtimeConfig.DataDir, "/bridge.db"))
	if err != nil {
		return err
	}

	eventTracker, err := tracker.NewEventTracker(
		&tracker.EventTrackerConfig{
			EventSubscriber:        b,
			Logger:                 b.logger,
			RPCEndpoint:            b.eventTrackerConfig.jsonrpcAddr,
			SyncBatchSize:          b.eventTrackerConfig.EventTracker.SyncBatchSize,
			NumBlockConfirmations:  b.eventTrackerConfig.EventTracker.NumBlockConfirmations,
			NumOfBlocksToReconcile: b.eventTrackerConfig.EventTracker.NumOfBlocksToReconcile,
			PollInterval:           b.eventTrackerConfig.trackerPollInterval,
			LogFilter: map[ethgo.Address][]ethgo.Hash{
				ethgo.Address(b.eventTrackerConfig.stateSenderAddr):       {stateSyncEventSig},
				ethgo.Address(b.eventTrackerConfig.checkpointManagerAddr): {checkpointSubmittedEventSig},
			},
		},
		store, b.eventTrackerConfig.startBlock,
	)

	if err != nil {
		return err
	}

	return eventTracker.Start()
}

// AddLog saves the received log from event tracker if it matches a state sync event ABI
func (b *bridgeManager) AddLog(eventLog *ethgo.Log) error {
	switch eventLog.Topics[0] {
	case stateSyncEventSig:
		return b.stateSyncManager.AddLog(eventLog)
	case checkpointSubmittedEventSig:
		return b.exitEventRelayer.AddLog(eventLog)
	default:
		b.logger.Error("Unknown event log receiver from event tracker")

		return nil
	}
}
