package polybft

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/Ethernal-Tech/ethgo"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	polybftProto "github.com/0xPolygon/polygon-edge/consensus/polybft/proto"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	errUnknownBridgeEvent = errors.New("unknown bridge event")
)

type Runtime interface {
	IsActiveValidator() bool
}

// BridgeEventManager is an interface that defines functions bridge message event workflow
type BridgeEventManager interface {
	EventSubscriber
	Init() error
	AddLog(chainID *big.Int, eventLog *ethgo.Log) error
	BridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error)
	PostBlock() error
	PostEpoch(req *PostEpochRequest) error
}

var _ BridgeEventManager = (*dummyBridgeEventManager)(nil)

// dummyBridgeEventManager is used when bridge is not enabled
type dummyBridgeEventManager struct{}

func (d *dummyBridgeEventManager) Init() error                                        { return nil }
func (d *dummyBridgeEventManager) AddLog(chainID *big.Int, eventLog *ethgo.Log) error { return nil }
func (d *dummyBridgeEventManager) BridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *dummyBridgeEventManager) PostBlock() error { return nil }
func (d *dummyBridgeEventManager) PostEpoch(req *PostEpochRequest) error {
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

// bridgeEventManagerConfig holds the configuration data of bridge event manager
type bridgeEventManagerConfig struct {
	bridgeCfg         *BridgeConfig
	topic             topic
	key               *wallet.Key
	maxNumberOfEvents uint64
}

var _ BridgeEventManager = (*bridgeEventManager)(nil)

// bridgeEventManager is a struct that manages the workflow of
// saving and querying bridge message events, and creating, and submitting new batches
type bridgeEventManager struct {
	logger hclog.Logger
	state  *State

	config *bridgeEventManagerConfig

	// per epoch fields
	lock                      sync.RWMutex
	pendingBridgeBatches      []*PendingBridgeBatch
	validatorSet              validator.ValidatorSet
	epoch                     uint64
	nextBridgeEventIDExternal uint64
	nextBridgeEventIDInternal uint64
	externalChainID           uint64
	internalChainID           uint64

	runtime Runtime
}

// topic is an interface for p2p message gossiping
type topic interface {
	Publish(obj proto.Message) error
	Subscribe(handler func(obj interface{}, from peer.ID)) error
}

// newBridgeEventManager creates a new instance of bridge event manager
func newBridgeEventManager(
	logger hclog.Logger,
	state *State,
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

// Init subscribes to bridge topics (getting votes)
func (b *bridgeEventManager) Init() error {
	if err := b.initTransport(); err != nil {
		return fmt.Errorf("failed to initialize bridge event transport layer. Error: %w", err)
	}

	return nil
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

		var transportMsg *TransportMessage

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
func (b *bridgeEventManager) saveVote(msg *TransportMessage) error {
	b.lock.RLock()
	epoch := b.epoch
	valSet := b.validatorSet
	b.lock.RUnlock()

	if valSet == nil || msg.EpochNumber < epoch || msg.EpochNumber > epoch+1 ||
		(b.externalChainID != msg.SourceChainID && b.internalChainID != msg.SourceChainID) {
		// Epoch metadata is undefined or received a message for the irrelevant epoch
		return nil
	}

	if msg.EpochNumber == epoch+1 {
		if err := b.state.EpochStore.insertEpoch(epoch+1, nil, msg.SourceChainID); err != nil {
			return fmt.Errorf("error saving msg vote from a future epoch: %d. Error: %w", epoch+1, err)
		}
	}

	if err := b.verifyVoteSignature(valSet, types.StringToAddress(msg.From), msg.Signature, msg.Hash); err != nil {
		return fmt.Errorf("error verifying vote signature: %w", err)
	}

	msgVote := &MessageSignature{
		From:      msg.From,
		Signature: msg.Signature,
	}

	numSignatures, err := b.state.BridgeMessageStore.insertMessageVote(
		msg.EpochNumber,
		msg.Hash,
		msgVote,
		nil,
		msg.SourceChainID)
	if err != nil {
		return fmt.Errorf("error inserting message vote: %w", err)
	}

	b.logger.Info(
		"deliver message",
		"hash", hex.EncodeToString(msg.Hash),
		"sender", msg.From,
		"signatures", numSignatures,
	)

	return nil
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

	if err := b.state.BridgeMessageStore.insertBridgeMessageEvent(event); err != nil {
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
	pendingBridgeBatch *PendingBridgeBatch) (Signature, error) {
	validatorSet := b.validatorSet

	validatorAddrToIndex := make(map[string]int, validatorSet.Len())
	validatorsMetadata := validatorSet.Accounts()

	for i, validator := range validatorsMetadata {
		validatorAddrToIndex[validator.Address.String()] = i
	}

	bridgeBatchHash, err := pendingBridgeBatch.Hash()
	if err != nil {
		return Signature{}, err
	}

	// get all the votes from the database for batch
	votes, err := b.state.BridgeMessageStore.getMessageVotes(
		pendingBridgeBatch.Epoch,
		bridgeBatchHash.Bytes(),
		pendingBridgeBatch.SourceChainID.Uint64())
	if err != nil {
		return Signature{}, err
	}

	var (
		signatures = make(bls.Signatures, 0, len(votes))
		bmap       = bitmap.Bitmap{}
		signers    = make(map[types.Address]struct{}, 0)
	)

	for _, vote := range votes {
		index, exists := validatorAddrToIndex[vote.From]
		if !exists {
			continue // don't count this vote, because it does not belong to validator
		}

		signature, err := bls.UnmarshalSignature(vote.Signature)
		if err != nil {
			return Signature{}, err
		}

		bmap.Set(uint64(index)) //nolint:gosec

		signatures = append(signatures, signature)
		signers[types.StringToAddress(vote.From)] = struct{}{}
	}

	if !validatorSet.HasQuorum(blockNumber, signers) {
		return Signature{}, errQuorumNotReached
	}

	aggregatedSignature, err := signatures.Aggregate().Marshal()
	if err != nil {
		return Signature{}, err
	}

	result := Signature{
		AggregatedSignature: aggregatedSignature,
		Bitmap:              bmap,
	}

	return result, nil
}

// PostEpoch notifies the bridge event manager that an epoch has changed,
// so that it can discard any previous epoch bridge batch, and build a new one (since validator set changed)
func (b *bridgeEventManager) PostEpoch(req *PostEpochRequest) error {
	b.lock.Lock()

	var err error

	b.pendingBridgeBatches = nil
	b.validatorSet = req.ValidatorSet
	b.epoch = req.NewEpochID

	// build a new batch at the end of the epoch
	b.nextBridgeEventIDExternal, err = req.SystemState.GetNextCommittedIndexExternal(b.externalChainID)
	if err != nil {
		b.lock.Unlock()

		return err
	}

	b.nextBridgeEventIDInternal, err = req.SystemState.GetNextCommittedIndexInternal(b.internalChainID)
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
	return b.buildBridgeBatch(dbTx, b.externalChainID, b.internalChainID, b.nextBridgeEventIDExternal)
}

// buildInternalBridgeBatch builds a new internal bridge batch, signs it and gossips its vote for it
func (b *bridgeEventManager) buildInternalBridgeBatch(dbTx *bolt.Tx) error {
	return b.buildBridgeBatch(dbTx, b.internalChainID, b.externalChainID, b.nextBridgeEventIDInternal)
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
	bridgeMessageEvents, err := b.state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(
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

	sig := &MessageSignature{
		From:      b.config.key.String(),
		Signature: signature,
	}

	if _, err = b.state.BridgeMessageStore.insertMessageVote(
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
	b.multicast(&TransportMessage{
		Hash:          hashBytes,
		Signature:     signature,
		From:          b.config.key.String(),
		EpochNumber:   epoch,
		SourceChainID: sourceChainID,
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
	var bridgeMessageResult contractsapi.BridgeMessageResultEvent

	var bridgeMsg contractsapi.BridgeMsgEvent

	return map[types.Address][]types.Hash{
		b.config.bridgeCfg.InternalGatewayAddr: {
			types.Hash(bridgeMsg.Sig()),
			types.Hash(bridgeMessageResult.Sig())},
	}
}

// ProcessLog is the implementation of EventSubscriber interface,
// used to handle a log defined in GetLogFilters, provided by event provider
func (b *bridgeEventManager) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	var (
		bridgeMessageResultEvent contractsapi.BridgeMessageResultEvent
		bridgeMsgEvent           contractsapi.BridgeMsgEvent
	)

	switch log.Topics[0] {
	case bridgeMessageResultEvent.Sig():
		doesMatch, err := bridgeMessageResultEvent.ParseLog(log)
		if err != nil {
			return err
		}

		if !doesMatch || b.externalChainID != bridgeMessageResultEvent.SourceChainID.Uint64() {
			return nil
		}

		return b.state.BridgeMessageStore.removeBridgeEvents(&bridgeMessageResultEvent)
	case bridgeMsgEvent.Sig():
		doesMatch, err := bridgeMsgEvent.ParseLog(log)
		if err != nil {
			return err
		}

		if !doesMatch || b.externalChainID != bridgeMessageResultEvent.DestinationChainID.Uint64() {
			return nil
		}

		return b.state.BridgeMessageStore.insertBridgeMessageEvent(&bridgeMsgEvent)
	default:
		return errUnknownBridgeEvent
	}
}
