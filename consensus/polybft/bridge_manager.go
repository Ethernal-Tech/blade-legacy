package polybft

import (
	"fmt"
	"math/big"
	"path"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/blockchain-event-tracker/store"
	"github.com/Ethernal-Tech/blockchain-event-tracker/tracker"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

const (
	// defaultMaxBlocksToWaitForResend specifies how many blocks should be wait
	// in order to try again to send transaction
	defaultMaxBlocksToWaitForResend = uint64(30)
	// defaultMaxAttemptsToSend specifies how many sending retries for one transaction
	defaultMaxAttemptsToSend = uint64(15)
	// defaultMaxEventsPerBatch specifies maximum events per one batchExecute tx
	defaultMaxEventsPerBatch = uint64(10)
)

var bridgeMessageEventSig = new(contractsapi.BridgeMsgEvent).Sig()

// RelayerEventMetaData keeps information about a relayer event
type RelayerEventMetaData struct {
	EventID            uint64 `json:"eventID"`
	CountTries         uint64 `json:"countTries"`
	BlockNumber        uint64 `json:"blockNumber"` // block when event is sent
	SentStatus         bool   `json:"sentStatus"`
	SourceChainID      uint64 `json:"sourceChainID"`
	DestinationChainID uint64 `json:"destinationChainID"`
}

func (ed RelayerEventMetaData) String() string {
	return fmt.Sprintf("%d", ed.EventID)
}

// relayerConfig is a struct that holds the relayer configuration
type relayerConfig struct {
	maxBlocksToWaitForResend uint64
	maxAttemptsToSend        uint64
	maxEventsPerBatch        uint64
	eventExecutionAddr       types.Address
}

// eventTrackerConfig is a struct that holds the event tracker configuration
type eventTrackerConfig struct {
	consensus.EventTracker

	gatewayAddr         types.Address
	jsonrpcAddr         string
	startBlock          uint64
	trackerPollInterval time.Duration
}

// RelayerState is an interface that defines functions that a relayer store has to implement
type RelayerState interface {
	GetAllAvailableRelayerEvents(limit int) (result []*RelayerEventMetaData, err error)
	UpdateRelayerEvents(events []*RelayerEventMetaData, removedEvents []*RelayerEventMetaData, dbTx *bolt.Tx) error
}

// relayerEventsProcessor is a parent struct of both bridge event and exit relayer
// that holds functions common to both relayers
type relayerEventsProcessor struct {
	logger     hclog.Logger
	state      RelayerState
	blockchain blockchainBackend

	config *relayerConfig
	sendTx func([]*RelayerEventMetaData) error
}

// ProcessEvents processes all relayer events that were either successfully or unsuccessfully executed
// and executes all the events that can be executed in regards to relayerConfig
func (r *relayerEventsProcessor) processEvents() {
	// we need twice as batch size because events from first batch are possible already sent maxAttemptsToSend times
	events, err := r.state.GetAllAvailableRelayerEvents(int(r.config.maxEventsPerBatch) * 2)
	if err != nil {
		r.logger.Error("retrieving events failed", "err", err)

		return
	}

	if len(events) == 0 {
		return
	}

	removedEventIDs := make([]*RelayerEventMetaData, 0, len(events))
	sendingEvents := make([]*RelayerEventMetaData, 0, len(events))
	currentBlockNumber := r.blockchain.CurrentHeader().Number

	// check already processed events
	for _, event := range events {
		// quit if we are still waiting for some old event confirmation (there is no parallelization right now!)
		if event.SentStatus && event.BlockNumber+r.config.maxBlocksToWaitForResend > currentBlockNumber {
			return
		}

		// remove event if it is processed too many times
		if event.CountTries+1 > r.config.maxAttemptsToSend {
			removedEventIDs = append(removedEventIDs, event)
		} else {
			event.CountTries++
			event.BlockNumber = currentBlockNumber
			event.SentStatus = true

			sendingEvents = append(sendingEvents, event)
			if len(sendingEvents) == int(r.config.maxEventsPerBatch) {
				break
			}
		}
	}

	// update state only if needed
	if len(sendingEvents)+len(removedEventIDs) > 0 {
		r.logger.Debug("updating relayer events storage", "events", sendingEvents, "removed", removedEventIDs)

		if err := r.state.UpdateRelayerEvents(sendingEvents, removedEventIDs, nil); err != nil {
			r.logger.Error("updating relayer events storage failed",
				"events", sendingEvents, "removed", removedEventIDs, "err", err)

			return
		}
	}

	// send tx only if needed
	if len(sendingEvents) > 0 {
		if err := r.sendTx(sendingEvents); err != nil {
			r.logger.Error("failed to send relayer tx", "block", currentBlockNumber, "events", sendingEvents, "err", err)
		} else {
			r.logger.Debug("relayer tx has been successfully sent", "block", currentBlockNumber, "events", sendingEvents)
		}
	}
}

// BridgeManager is an interface that defines functions that a bridge manager must implement
type BridgeManager interface {
	tracker.EventSubscriber

	Close()
	PostBlock(req *PostBlockRequest) error
	PostEpoch(req *PostEpochRequest) error
	BridgeBatch(pendingBlockNumber uint64) (*BridgeBatchSigned, error)
	InsertEpoch(epoch uint64, tx *bolt.Tx) error
}

var _ BridgeManager = (*dummyBridgeManager)(nil)

type dummyBridgeManager struct{}

func (d *dummyBridgeManager) Close()                                        {}
func (d *dummyBridgeManager) AddLog(chainID *big.Int, log *ethgo.Log) error { return nil }
func (d *dummyBridgeManager) PostBlock(req *PostBlockRequest) error         { return nil }
func (d *dummyBridgeManager) PostEpoch(req *PostEpochRequest) error         { return nil }
func (d *dummyBridgeManager) BuildExitEventRoot(epoch uint64) (types.Hash, error) {
	return types.ZeroHash, nil
}
func (d *dummyBridgeManager) BridgeBatch(pendingBlockNumber uint64) (*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *dummyBridgeManager) InsertEpoch(epoch uint64, tx *bolt.Tx) error { return nil }

var _ BridgeManager = (*bridgeManager)(nil)

// bridgeManager is a struct that manages different bridge components
// such as handling and executing bridge events
type bridgeManager struct {
	bridgeEventManager BridgeEventManager
	stateSyncRelayer   StateSyncRelayer

	eventTracker       *tracker.EventTracker
	eventTrackerConfig *eventTrackerConfig
	logger             hclog.Logger
	externalChainID    uint64
	internalChainID    uint64
	state              *State
}

// newBridgeManager creates a new instance of bridgeManager
func newBridgeManager(
	runtime Runtime,
	runtimeConfig *runtimeConfig,
	eventProvider *EventProvider,
	logger hclog.Logger,
	chainID uint64) (BridgeManager, error) {
	if !runtimeConfig.GenesisConfig.IsBridgeEnabled() {
		return &dummyBridgeManager{}, nil
	}

	var err error

	gatewayAddr := runtimeConfig.GenesisConfig.Bridge[chainID].ExternalGatewayAddr
	bridgeManager := &bridgeManager{
		externalChainID: chainID,
		logger:          logger.Named("bridge-manager"),
		eventTrackerConfig: &eventTrackerConfig{
			EventTracker:        *runtimeConfig.eventTracker,
			jsonrpcAddr:         runtimeConfig.GenesisConfig.Bridge[chainID].JSONRPCEndpoint,
			startBlock:          runtimeConfig.GenesisConfig.Bridge[chainID].EventTrackerStartBlocks[gatewayAddr],
			trackerPollInterval: runtimeConfig.GenesisConfig.BlockTrackerPollInterval.Duration,
		},
		state: runtimeConfig.State,
	}

	if err := bridgeManager.initBridgeEventManager(eventProvider, runtime, runtimeConfig, logger); err != nil {
		return nil, err
	}

	if err := bridgeManager.initStateSyncRelayer(eventProvider, runtimeConfig, logger); err != nil {
		return nil, err
	}

	if bridgeManager.eventTracker, err = bridgeManager.initTracker(runtimeConfig); err != nil {
		return nil, fmt.Errorf("failed to init event tracker. Error: %w", err)
	}

	return bridgeManager, nil
}

// PostBlock is a function executed on every block finalization (either by consensus or syncer)
func (b *bridgeManager) PostBlock(req *PostBlockRequest) error {
	if err := b.bridgeEventManager.PostBlock(); err != nil {
		return fmt.Errorf("failed to execute post block in bridge event manager. Err: %w", err)
	}

	if err := b.stateSyncRelayer.PostBlock(req); err != nil {
		return fmt.Errorf("failed to execute post block in state sync relayer. Err: %w", err)
	}

	return nil
}

// PostEpoch is a function executed on epoch ending / start of new epoch
func (b *bridgeManager) PostEpoch(req *PostEpochRequest) error {
	if err := b.bridgeEventManager.PostEpoch(req); err != nil {
		return fmt.Errorf("failed to execute post epoch in bridge event manager. Error: %w", err)
	}

	return nil
}

// BridgeBatch returns the pending signed bridge batch
func (b *bridgeManager) BridgeBatch(pendingBlockNumber uint64) (*BridgeBatchSigned, error) {
	return b.bridgeEventManager.BridgeBatch(pendingBlockNumber)
}

// close stops ongoing go routines in the manager
func (b *bridgeManager) Close() {
	b.stateSyncRelayer.Close()
	b.eventTracker.Close()
}

// initBridgeEventManager initializes bridge event manager
// if bridge is not enabled, then a dummy bridge event manager will be used
func (b *bridgeManager) initBridgeEventManager(
	eventProvider *EventProvider,
	runtime Runtime,
	runtimeConfig *runtimeConfig,
	logger hclog.Logger) error {
	bridgeEventManager := newBridgeEventManager(
		logger.Named("bridge-event-manager"),
		runtimeConfig.State,
		&bridgeEventManagerConfig{
			bridgeCfg:         runtimeConfig.GenesisConfig.Bridge[b.externalChainID],
			key:               runtimeConfig.Key,
			topic:             runtimeConfig.bridgeTopic,
			maxNumberOfEvents: maxNumberOfEvents,
		},
		runtime,
		b.externalChainID,
		b.internalChainID,
	)

	eventProvider.Subscribe(b.bridgeEventManager)

	b.bridgeEventManager = bridgeEventManager

	return b.bridgeEventManager.Init()
}

// initStateSyncRelayer initializes bridge event relayer
// if not enabled, then a dummy bridge event relayer will be used
func (b *bridgeManager) initStateSyncRelayer(
	eventProvider *EventProvider,
	runtimeConfig *runtimeConfig,
	logger hclog.Logger) error {
	if runtimeConfig.consensusConfig.IsRelayer {
		txRelayer, err := getBridgeTxRelayer(runtimeConfig.consensusConfig.RPCEndpoint, logger)
		if err != nil {
			return err
		}

		b.stateSyncRelayer = newStateSyncRelayer(
			txRelayer,
			runtimeConfig.State.BridgeMessageStore,
			runtimeConfig.blockchain,
			wallet.NewEcdsaSigner(runtimeConfig.Key),
			&relayerConfig{
				maxBlocksToWaitForResend: defaultMaxBlocksToWaitForResend,
				maxAttemptsToSend:        defaultMaxAttemptsToSend,
				maxEventsPerBatch:        defaultMaxEventsPerBatch,
				//eventExecutionAddr:       contracts.GatewayContract,
			},
			logger.Named("state_sync_relayer"))
	} else {
		b.stateSyncRelayer = &dummyStateSyncRelayer{}
	}

	eventProvider.Subscribe(b.stateSyncRelayer)

	return b.stateSyncRelayer.Init()
}

// initTracker starts a new event tracker (to receive bridge events)
func (b *bridgeManager) initTracker(runtimeConfig *runtimeConfig) (*tracker.EventTracker, error) {
	store, err := store.NewBoltDBEventTrackerStore(path.Join(runtimeConfig.DataDir, "/bridge.db"))
	if err != nil {
		return nil, err
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
				ethgo.Address(b.eventTrackerConfig.gatewayAddr): {bridgeMessageEventSig},
			},
		},
		store, b.eventTrackerConfig.startBlock,
	)

	if err != nil {
		return nil, err
	}

	return eventTracker, eventTracker.Start()
}

// AddLog saves the received log from event tracker if it matches a bridge message event ABI
func (b *bridgeManager) AddLog(chainID *big.Int, eventLog *ethgo.Log) error {
	switch eventLog.Topics[0] {
	case bridgeMessageEventSig:
		return b.bridgeEventManager.AddLog(chainID, eventLog)
	default:
		b.logger.Error("Unknown event log receiver from event tracker")

		return nil
	}
}

// InsertEpoch inserts a new epoch to db with its meta data
func (b *bridgeManager) InsertEpoch(epochNumber uint64, dbTx *bolt.Tx) error {
	if err := b.state.EpochStore.insertEpoch(epochNumber, dbTx, b.externalChainID); err != nil {
		return fmt.Errorf("an error occurred while inserting new epoch in db, chainID: %d. Reason: %w",
			b.externalChainID, err)
	}

	return nil
}
