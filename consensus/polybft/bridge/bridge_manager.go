package bridge

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path"
	"sync"

	"github.com/Ethernal-Tech/blockchain-event-tracker/store"
	"github.com/Ethernal-Tech/blockchain-event-tracker/tracker"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
	bolt "go.etcd.io/bbolt"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	polybftProto "github.com/0xPolygon/polygon-edge/consensus/polybft/proto"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	errUnknownBridgeEvent = errors.New("unknown bridge event")

	// Bridge events signatures
	bridgeMessageEventSig       = new(contractsapi.BridgeMsgEvent).Sig()
	bridgeMessageResultEventSig = new(contractsapi.BridgeMessageResultEvent).Sig()
	newBatchEventSig            = new(contractsapi.NewBatchEvent).Sig()
	newValidatorSetEventSig     = new(contractsapi.NewValidatorSetEvent).Sig()
)

const maxNumberOfBatchEvents = 10

type Runtime interface {
	IsActiveValidator() bool
}

// BridgeManager is an interface that defines functions for bridge workflow
type BridgeManager interface {
	state.EventSubscriber
	Start(runtimeCfg *config.Runtime) error
	AddLog(chainID *big.Int, eventLog *ethgo.Log) error
	BridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error)
	PostBlock() error
	PostEpoch(req *polytypes.PostEpochRequest) error
	Close()
}

var _ BridgeManager = (*dummyBridgeEventManager)(nil)

// dummyBridgeEventManager is used when bridge is not enabled
type dummyBridgeEventManager struct{}

func (d *dummyBridgeEventManager) Start(runtimeCfg *config.Runtime) error             { return nil }
func (d *dummyBridgeEventManager) AddLog(chainID *big.Int, eventLog *ethgo.Log) error { return nil }
func (d *dummyBridgeEventManager) BridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *dummyBridgeEventManager) PostBlock() error { return nil }
func (d *dummyBridgeEventManager) PostEpoch(req *polytypes.PostEpochRequest) error {
	return nil
}

// EventSubscriber implementation
func (d *dummyBridgeEventManager) GetLogFilters() map[types.Address][]types.Hash {
	return make(map[types.Address][]types.Hash)
}
func (d *dummyBridgeEventManager) ProcessLog(header *types.Header,
	log *ethgo.Log, dbTx *bolt.Tx) error {
	return nil
}
func (d *dummyBridgeEventManager) Close() {}

// bridgeEventManagerConfig holds the configuration data of bridge event manager
type bridgeEventManagerConfig struct {
	bridgeCfg         *config.Bridge
	topic             Topic
	key               *wallet.Key
	maxNumberOfEvents uint64
}

var _ BridgeManager = (*bridgeEventManager)(nil)

// bridgeEventManager is a struct that manages the workflow of
// saving and querying bridge message events, and creating, and submitting new batches
type bridgeEventManager struct {
	logger hclog.Logger
	state  *BridgeManagerStore

	config *bridgeEventManagerConfig

	// per epoch fields
	lock                 sync.RWMutex
	pendingBridgeBatches []*PendingBridgeBatch
	validatorSet         validator.ValidatorSet
	epoch                uint64
	nextEventIDExternal  uint64
	nextEventIDInternal  uint64
	externalChainID      uint64
	internalChainID      uint64

	runtime Runtime
	tracker *tracker.EventTracker
}

// newBridgeManager creates a new instance of bridge event manager
func newBridgeManager(
	logger hclog.Logger,
	state *BridgeManagerStore,
	config *bridgeEventManagerConfig,
	runtime Runtime,
	externalChainID, internalChainID uint64) *bridgeEventManager {
	return &bridgeEventManager{
		logger:          logger,
		state:           state,
		config:          config,
		runtime:         runtime,
		externalChainID: externalChainID,
		internalChainID: internalChainID,
	}
}

// Start starts the bridge event manager
func (b *bridgeEventManager) Start(runtimeConfig *config.Runtime) error {
	if err := b.initTransport(); err != nil {
		return fmt.Errorf("failed to initialize bridge event transport layer. Error: %w", err)
	}

	tracker, err := b.initTracker(runtimeConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize bridge event tracker. Error: %w", err)
	}

	b.tracker = tracker

	return nil
}

// Close stops the bridge manager
func (b *bridgeEventManager) Close() {
	b.tracker.Close()
}

// initTracker starts a new event tracker (to receive bridge events from external chain)
func (b *bridgeEventManager) initTracker(runtimeCfg *config.Runtime) (*tracker.EventTracker, error) {
	store, err := store.NewBoltDBEventTrackerStore(path.Join(runtimeCfg.StateDataDir, "/bridge.db"))
	if err != nil {
		return nil, err
	}

	eventTracker, err := tracker.NewEventTracker(
		&tracker.EventTrackerConfig{
			EventSubscriber:        b,
			Logger:                 b.logger,
			RPCEndpoint:            b.config.bridgeCfg.JSONRPCEndpoint,
			SyncBatchSize:          runtimeCfg.EventTracker.SyncBatchSize,
			NumBlockConfirmations:  runtimeCfg.EventTracker.NumBlockConfirmations,
			NumOfBlocksToReconcile: runtimeCfg.EventTracker.NumOfBlocksToReconcile,
			PollInterval:           runtimeCfg.GenesisConfig.BlockTrackerPollInterval.Duration,
			LogFilter: map[ethgo.Address][]ethgo.Hash{
				ethgo.Address(b.config.bridgeCfg.ExternalGatewayAddr): {bridgeMessageEventSig},
			},
		},
		store, b.config.bridgeCfg.EventTrackerStartBlocks[b.config.bridgeCfg.ExternalGatewayAddr],
	)

	if err != nil {
		return nil, err
	}

	return eventTracker, eventTracker.Start()
}

// initTransport subscribes to bridge topics (getting votes for batches)
func (b *bridgeEventManager) initTransport() error {
	return b.config.topic.Subscribe(func(obj interface{}, _ peer.ID) {
		if !b.runtime.IsActiveValidator() {
			// don't save votes if not a validator
			return
		}

		msg, ok := obj.(*polybftProto.TransportMessage)
		if !ok {
			b.logger.Warn("failed to deliver vote, invalid msg", "obj", obj)

			return
		}

		var transportMsg *BridgeBatchVote
		if err := json.Unmarshal(msg.Data, &transportMsg); err != nil {
			b.logger.Warn("failed to deliver vote", "error", err)

			return
		}

		if err := b.saveVote(transportMsg); err != nil {
			b.logger.Warn("failed to deliver vote", "error", err)
		}
	})
}

// saveVote saves the gotten vote to boltDb for later quorum check and signature aggregation
func (b *bridgeEventManager) saveVote(vote *BridgeBatchVote) error {
	b.lock.RLock()
	epoch := b.epoch
	valSet := b.validatorSet
	b.lock.RUnlock()

	if valSet == nil || vote.EpochNumber < epoch || vote.EpochNumber > epoch+1 {
		// Epoch metadata is undefined or received a vote for the irrelevant epoch
		return nil
	}

	if !b.isRelevantChainID(vote.SourceChainID) || !b.isRelevantChainID(vote.DestinationChainID) {
		// Vote is for irrelevant chain, skip it
		return nil
	}

	if vote.EpochNumber == epoch+1 {
		if err := b.state.insertEpoch(epoch+1, nil, vote.SourceChainID); err != nil {
			return fmt.Errorf("error saving msg vote from a future epoch: %d. Error: %w", epoch+1, err)
		}
	}

	if err := b.verifyVoteSignature(valSet, types.StringToAddress(vote.Sender), vote.Signature, vote.Hash); err != nil {
		return fmt.Errorf("error verifying vote signature: %w", err)
	}

	msgVote := &BridgeBatchVoteConsensusData{
		Sender:    vote.Sender,
		Signature: vote.Signature,
	}

	numSignatures, err := b.state.insertConsensusData(
		vote.EpochNumber,
		vote.Hash,
		msgVote,
		nil,
		vote.SourceChainID)
	if err != nil {
		return fmt.Errorf("error inserting message vote: %w", err)
	}

	b.logger.Info(
		"deliver message",
		"hash", hex.EncodeToString(vote.Hash),
		"sender", vote.Sender,
		"signatures", numSignatures,
	)

	return nil
}

// isRelevantChainID checks whether internal or external chain id corresponds to the given chain id
func (b *bridgeEventManager) isRelevantChainID(chainID uint64) bool {
	return b.internalChainID == chainID || b.externalChainID == chainID
}

// Verifies signature of the message against the public key of the signer and checks if the signer is a validator
func (b *bridgeEventManager) verifyVoteSignature(valSet validator.ValidatorSet, signerAddr types.Address,
	signature []byte, hash []byte) error {
	validator := valSet.Accounts().GetValidatorMetadata(signerAddr)
	if validator == nil {
		return fmt.Errorf("unable to resolve validator %s", signerAddr)
	}

	unmarshaledSignature, err := bls.UnmarshalSignature(signature)
	if err != nil {
		return fmt.Errorf("failed to unmarshal signature from signer %s, %w", signerAddr.String(), err)
	}

	if !unmarshaledSignature.Verify(validator.BlsKey, hash, signer.DomainBridge) {
		return fmt.Errorf("incorrect signature from %s", signerAddr)
	}

	return nil
}

// AddLog saves the received log from event tracker if it matches a bridge message event ABI
func (b *bridgeEventManager) AddLog(chainID *big.Int, eventLog *ethgo.Log) error {
	if b.externalChainID != chainID.Uint64() {
		return nil
	}

	event := &contractsapi.BridgeMsgEvent{}

	doesMatch, err := event.ParseLog(eventLog)
	if !doesMatch {
		return nil
	}

	b.logger.Info(
		"Add Bridge message event",
		"block", eventLog.BlockNumber,
		"hash", eventLog.TransactionHash,
		"index", eventLog.LogIndex,
	)

	if err != nil {
		b.logger.Error("could not decode bridge message event", "err", err)

		return err
	}

	if err := b.state.insertBridgeMessageEvent(event); err != nil {
		b.logger.Error("could not save bridge message event to boltDb", "err", err)

		return err
	}

	if err := b.buildExternalBridgeBatch(nil); err != nil {
		// we don't return an error here. If bridge message event is inserted in db,
		// we will just try to build a batch on next block or next event arrival
		b.logger.Error("could not build a batch on arrival of new bridge message event",
			"err", err, "bridgeMessageID", event.ID)
	}

	return nil
}

// BridgeBatch returns a batch to be submitted if there is a pending batch with quorum
func (b *bridgeEventManager) BridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()

	var largestBridgeBatch *BridgeBatchSigned

	// we start from the end, since last pending batch is the largest one
	for i := len(b.pendingBridgeBatches) - 1; i >= 0; i-- {
		pendingBatch := b.pendingBridgeBatches[i]
		aggregatedSignature, err := b.getAggSignatureForBridgeBatchMessage(blockNumber, pendingBatch)
		numberOfMessages := len(pendingBatch.Messages)

		if err != nil {
			if errors.Is(err, errQuorumNotReached) {
				// a valid case, batch has no quorum, we should not return an error
				if numberOfMessages > 0 {
					b.logger.Debug("can not submit a batch, quorum not reached",
						"from", pendingBatch.BridgeMessageBatch.Messages[0].ID.Uint64(),
						"to", pendingBatch.BridgeMessageBatch.Messages[numberOfMessages-1].ID.Uint64())
				}

				continue
			}

			return nil, err
		}

		largestBridgeBatch = &BridgeBatchSigned{
			MessageBatch: pendingBatch.BridgeMessageBatch,
			AggSignature: aggregatedSignature,
		}

		break
	}

	return largestBridgeBatch, nil
}

// getAggSignatureForBridgeBatchMessage checks if pending batch has quorum,
// and if it does, aggregates the signatures
func (b *bridgeEventManager) getAggSignatureForBridgeBatchMessage(blockNumber uint64,
	pendingBridgeBatch *PendingBridgeBatch) (polytypes.Signature, error) {
	validatorSet := b.validatorSet

	validatorAddrToIndex := make(map[string]int, validatorSet.Len())
	validatorsMetadata := validatorSet.Accounts()

	for i, validator := range validatorsMetadata {
		validatorAddrToIndex[validator.Address.String()] = i
	}

	bridgeBatchHash, err := pendingBridgeBatch.Hash()
	if err != nil {
		return polytypes.Signature{}, err
	}

	// get all the votes from the database for batch
	votes, err := b.state.getMessageVotes(
		pendingBridgeBatch.Epoch,
		bridgeBatchHash.Bytes(),
		pendingBridgeBatch.SourceChainID.Uint64())
	if err != nil {
		return polytypes.Signature{}, err
	}

	var (
		signatures = make(bls.Signatures, 0, len(votes))
		bmap       = bitmap.Bitmap{}
		signers    = make(map[types.Address]struct{}, 0)
	)

	for _, vote := range votes {
		index, exists := validatorAddrToIndex[vote.Sender]
		if !exists {
			continue // don't count this vote, because it does not belong to validator
		}

		signature, err := bls.UnmarshalSignature(vote.Signature)
		if err != nil {
			return polytypes.Signature{}, err
		}

		bmap.Set(uint64(index)) //nolint:gosec

		signatures = append(signatures, signature)
		signers[types.StringToAddress(vote.Sender)] = struct{}{}
	}

	if !validatorSet.HasQuorum(blockNumber, signers) {
		return polytypes.Signature{}, errQuorumNotReached
	}

	aggregatedSignature, err := signatures.Aggregate().Marshal()
	if err != nil {
		return polytypes.Signature{}, err
	}

	result := polytypes.Signature{
		AggregatedSignature: aggregatedSignature,
		Bitmap:              bmap,
	}

	return result, nil
}

// PostEpoch notifies the bridge event manager that an epoch has changed,
// so that it can discard any previous epoch bridge batch, and build a new one (since validator set changed)
func (b *bridgeEventManager) PostEpoch(req *polytypes.PostEpochRequest) error {
	if err := b.state.insertEpoch(req.NewEpochID, req.DBTx, b.externalChainID); err != nil {
		return fmt.Errorf("an error occurred while inserting new epoch in db, chainID: %d. Reason: %w",
			b.externalChainID, err)
	}

	b.lock.Lock()

	var err error

	b.pendingBridgeBatches = nil
	b.validatorSet = req.ValidatorSet
	b.epoch = req.NewEpochID

	// build a new batch at the end of the epoch
	b.nextEventIDExternal, err = req.SystemState.GetNextCommittedIndex(b.externalChainID, systemstate.External)
	if err != nil {
		b.lock.Unlock()

		return err
	}

	b.nextEventIDInternal, err = req.SystemState.GetNextCommittedIndex(b.internalChainID, systemstate.Internal)
	if err != nil {
		b.lock.Unlock()

		return err
	}

	b.lock.Unlock()

	if err := b.buildInternalBridgeBatch(req.DBTx); err != nil {
		return err
	}

	return b.buildExternalBridgeBatch(req.DBTx)
}

// PostBlock creates batch from internal events.
func (b *bridgeEventManager) PostBlock() error {
	if err := b.buildInternalBridgeBatch(nil); err != nil {
		// we don't return an error here. If bridge message event is inserted in db,
		// we will just try to build a batch on next block or next event arrival
		b.logger.Error("could not build a blade originated batch on PostBlock",
			"err", err)
	}

	return nil
}

// buildExternalBridgeBatch builds a new external bridge batch, signs it and gossips its vote for it
func (b *bridgeEventManager) buildExternalBridgeBatch(dbTx *bolt.Tx) error {
	return b.buildBridgeBatch(dbTx, b.externalChainID, b.internalChainID, b.nextEventIDExternal)
}

// buildInternalBridgeBatch builds a new internal bridge batch, signs it and gossips its vote for it
func (b *bridgeEventManager) buildInternalBridgeBatch(dbTx *bolt.Tx) error {
	return b.buildBridgeBatch(dbTx, b.internalChainID, b.externalChainID, b.nextEventIDInternal)
}

func (b *bridgeEventManager) buildBridgeBatch(
	dbTx *bolt.Tx,
	sourceChainID, destinationChainID uint64,
	nextBridgeEventIDIndex uint64) error {
	if !b.runtime.IsActiveValidator() {
		// don't build batch if not a validator
		return nil
	}

	b.lock.RLock()

	// Since lock is reduced grab original values into local variables in order to keep them
	epoch := b.epoch
	bridgeMessageEvents, err := b.state.getBridgeMessageEventsForBridgeBatch(
		nextBridgeEventIDIndex,
		nextBridgeEventIDIndex+b.config.maxNumberOfEvents-1,
		dbTx,
		sourceChainID, destinationChainID)

	if err != nil && !errors.Is(err, errNotEnoughBridgeEvents) {
		b.lock.RUnlock()

		return fmt.Errorf("failed to get bridge message event for batch. Error: %w", err)
	}

	if len(bridgeMessageEvents) == 0 {
		// there are no bridge message events
		b.lock.RUnlock()

		return nil
	}

	if len(b.pendingBridgeBatches) > 0 &&
		b.pendingBridgeBatches[len(b.pendingBridgeBatches)-1].
			BridgeMessageBatch.Messages[0].ID.
			Cmp(bridgeMessageEvents[len(bridgeMessageEvents)-1].ID) >= 0 {
		// already built a bridge batch of this size which is pending to be submitted
		b.lock.RUnlock()

		return nil
	}

	b.lock.RUnlock()

	pendingBridgeBatch, err := NewPendingBridgeBatch(epoch, bridgeMessageEvents)
	if err != nil {
		return err
	}

	hash, err := pendingBridgeBatch.Hash()
	if err != nil {
		return fmt.Errorf("failed to generate hash for BridgeBatch. Error: %w", err)
	}

	hashBytes := hash.Bytes()

	signature, err := b.config.key.SignWithDomain(hashBytes, signer.DomainBridge)
	if err != nil {
		return fmt.Errorf("failed to sign batch message. Error: %w", err)
	}

	sig := &BridgeBatchVoteConsensusData{
		Sender:    b.config.key.String(),
		Signature: signature,
	}

	if _, err = b.state.insertConsensusData(
		epoch,
		hashBytes,
		sig,
		dbTx,
		sourceChainID); err != nil {
		return fmt.Errorf(
			"failed to insert signature for message batch to the state. Error: %w",
			err,
		)
	}

	// gossip message
	b.multicast(&BridgeBatchVote{
		Hash: hashBytes,
		BridgeBatchVoteConsensusData: &BridgeBatchVoteConsensusData{
			Signature: signature,
			Sender:    b.config.key.String(),
		},
		EpochNumber:        epoch,
		SourceChainID:      sourceChainID,
		DestinationChainID: destinationChainID,
	})

	numberOfMessages := len(pendingBridgeBatch.BridgeMessageBatch.Messages)
	if numberOfMessages > 0 {
		b.logger.Debug(
			"[buildBridgeBatch] build batch",
			"from", pendingBridgeBatch.BridgeMessageBatch.Messages[0].ID.Uint64(),
			"to", pendingBridgeBatch.BridgeMessageBatch.Messages[numberOfMessages-1].ID.Uint64(),
		)
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	b.pendingBridgeBatches = append(b.pendingBridgeBatches, pendingBridgeBatch)

	return nil
}

// multicast publishes given message to the rest of the network
func (b *bridgeEventManager) multicast(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		b.logger.Warn("failed to marshal bridge message", "err", err)

		return
	}

	err = b.config.topic.Publish(&polybftProto.TransportMessage{Data: data})
	if err != nil {
		b.logger.Warn("failed to gossip bridge message", "err", err)
	}
}

// EventSubscriber implementation

// GetLogFilters returns a map of log filters for getting desired events,
// where the key is the address of contract that emits desired events,
// and the value is a slice of signatures of events we want to get.
// This function is the implementation of EventSubscriber interface
func (b *bridgeEventManager) GetLogFilters() map[types.Address][]types.Hash {
	return map[types.Address][]types.Hash{
		b.config.bridgeCfg.InternalGatewayAddr: {
			types.Hash(bridgeMessageEventSig),
			types.Hash(bridgeMessageResultEventSig)},
	}
}

// ProcessLog is the implementation of EventSubscriber interface,
// used to handle a log defined in GetLogFilters, provided by event provider
func (b *bridgeEventManager) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	switch log.Topics[0] {
	case bridgeMessageResultEventSig:
		var bridgeMessageResultEvent contractsapi.BridgeMessageResultEvent

		doesMatch, err := bridgeMessageResultEvent.ParseLog(log)
		if err != nil {
			return err
		}

		if !doesMatch || b.externalChainID != bridgeMessageResultEvent.SourceChainID.Uint64() {
			return nil
		}

		if bridgeMessageResultEvent.Status {
			return b.state.removeBridgeEvents(bridgeMessageResultEvent)
		}

		return nil
	case bridgeMessageEventSig:
		var bridgeMsgEvent contractsapi.BridgeMsgEvent

		doesMatch, err := bridgeMsgEvent.ParseLog(log)
		if err != nil {
			return err
		}

		if !doesMatch || b.externalChainID != bridgeMsgEvent.DestinationChainID.Uint64() {
			return nil
		}

		return b.state.insertBridgeMessageEvent(&bridgeMsgEvent)
	default:
		return errUnknownBridgeEvent
	}
}
