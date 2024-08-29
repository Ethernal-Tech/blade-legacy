package polybft

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
)

type Runtime interface {
	IsActiveValidator() bool
}

// StateSyncManager is an interface that defines functions for state sync workflow
type StateSyncManager interface {
	EventSubscriber
	Init() error
	AddLog(eventLog *ethgo.Log) error
	GetVotedBridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error)
	PostBlock(req *PostBlockRequest) error
	PostEpoch(req *PostEpochRequest, destinationChainID uint64) error
}

var _ StateSyncManager = (*dummyStateSyncManager)(nil)

// dummyStateSyncManager is used when bridge is not enabled
type dummyStateSyncManager struct{}

func (d *dummyStateSyncManager) Init() error                      { return nil }
func (d *dummyStateSyncManager) AddLog(eventLog *ethgo.Log) error { return nil }
func (d *dummyStateSyncManager) GetVotedBridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *dummyStateSyncManager) PostBlock(req *PostBlockRequest) error { return nil }
func (d *dummyStateSyncManager) PostEpoch(req *PostEpochRequest, chainID uint64) error {
	return nil
}

// EventSubscriber implementation
func (d *dummyStateSyncManager) GetLogFilters() map[types.Address][]types.Hash {
	return make(map[types.Address][]types.Hash)
}
func (d *dummyStateSyncManager) ProcessLog(header *types.Header,
	log *ethgo.Log, dbTx *bolt.Tx) error {
	return nil
}

// stateSyncConfig holds the configuration data of state sync manager
type stateSyncConfig struct {
	dataDir           string
	topic             topic
	key               *wallet.Key
	maxCommitmentSize uint64
}

var _ StateSyncManager = (*bridgeEventManager)(nil)

// bridgeEventManager is a struct that manages the workflow of
// saving and querying state sync events, and creating, and submitting new commitments
type bridgeEventManager struct {
	logger hclog.Logger
	state  *State

	config *stateSyncConfig

	// per epoch fields
	lock                   sync.RWMutex
	pendingBridgeBatches   []*PendingBridgeBatch
	validatorSet           validator.ValidatorSet
	epoch                  uint64
	nextBridgeEventIdIndex map[uint64]uint64

	runtime Runtime
}

// topic is an interface for p2p message gossiping
type topic interface {
	Publish(obj proto.Message) error
	Subscribe(handler func(obj interface{}, from peer.ID)) error
}

// newBridgeEventManager creates a new instance of state sync manager
func newBridgeEventManager(logger hclog.Logger, state *State, config *stateSyncConfig,
	runtime Runtime) *bridgeEventManager {
	return &bridgeEventManager{
		logger:                 logger,
		state:                  state,
		config:                 config,
		runtime:                runtime,
		nextBridgeEventIdIndex: make(map[uint64]uint64),
	}
}

// Init subscribes to bridge topics (getting votes) and start the event tracker routine
func (s *bridgeEventManager) Init() error {
	if err := s.initTransport(); err != nil {
		return fmt.Errorf("failed to initialize state sync transport layer. Error: %w", err)
	}

	return nil
}

// initTransport subscribes to bridge topics (getting votes for commitments)
func (s *bridgeEventManager) initTransport() error {
	return s.config.topic.Subscribe(func(obj interface{}, _ peer.ID) {
		if !s.runtime.IsActiveValidator() {
			// don't save votes if not a validator
			return
		}

		msg, ok := obj.(*polybftProto.TransportMessage)
		if !ok {
			s.logger.Warn("failed to deliver vote, invalid msg", "obj", obj)

			return
		}

		var transportMsg *TransportMessage

		if err := json.Unmarshal(msg.Data, &transportMsg); err != nil {
			s.logger.Warn("failed to deliver vote", "error", err)

			return
		}

		if err := s.saveVote(transportMsg); err != nil {
			s.logger.Warn("failed to deliver vote", "error", err)
		}
	})
}

// saveVote saves the gotten vote to boltDb for later quorum check and signature aggregation
func (s *bridgeEventManager) saveVote(msg *TransportMessage) error {
	s.lock.RLock()
	epoch := s.epoch
	valSet := s.validatorSet
	s.lock.RUnlock()

	if valSet == nil || msg.EpochNumber < epoch || msg.EpochNumber > epoch+1 {
		// Epoch metadata is undefined or received a message for the irrelevant epoch
		return nil
	}

	if msg.EpochNumber == epoch+1 {
		if err := s.state.EpochStore.insertEpoch(epoch+1, nil, msg.DestinationChainID); err != nil {
			return fmt.Errorf("error saving msg vote from a future epoch: %d. Error: %w", epoch+1, err)
		}
	}

	if err := s.verifyVoteSignature(valSet, types.StringToAddress(msg.From), msg.Signature, msg.BatchByte); err != nil {
		return fmt.Errorf("error verifying vote signature: %w", err)
	}

	msgVote := &MessageSignature{
		From:      msg.From,
		Signature: msg.Signature,
	}

	numSignatures, err := s.state.BridgeMessageStore.insertMessageVote(
		msg.EpochNumber,
		msg.BatchByte,
		msgVote,
		nil,
		msg.DestinationChainID)
	if err != nil {
		return fmt.Errorf("error inserting message vote: %w", err)
	}

	s.logger.Info(
		"deliver message",
		"hash", hex.EncodeToString(msg.BatchByte),
		"sender", msg.From,
		"signatures", numSignatures,
	)

	return nil
}

// Verifies signature of the message against the public key of the signer and checks if the signer is a validator
func (s *bridgeEventManager) verifyVoteSignature(valSet validator.ValidatorSet, signerAddr types.Address,
	signature []byte, hash []byte) error {
	validator := valSet.Accounts().GetValidatorMetadata(signerAddr)
	if validator == nil {
		return fmt.Errorf("unable to resolve validator %s", signerAddr)
	}

	unmarshaledSignature, err := bls.UnmarshalSignature(signature)
	if err != nil {
		return fmt.Errorf("failed to unmarshal signature from signer %s, %w", signerAddr.String(), err)
	}

	if !unmarshaledSignature.Verify(validator.BlsKey, hash, signer.DomainStateReceiver) {
		return fmt.Errorf("incorrect signature from %s", signerAddr)
	}

	return nil
}

// AddLog saves the received log from event tracker if it matches a bridge message event ABI
func (s *bridgeEventManager) AddLog(eventLog *ethgo.Log) error {
	event := &contractsapi.BridgeMessageEventEvent{}

	doesMatch, err := event.ParseLog(eventLog)
	if !doesMatch {
		return nil
	}

	s.logger.Info(
		"Add Bridge message event",
		"block", eventLog.BlockNumber,
		"hash", eventLog.TransactionHash,
		"index", eventLog.LogIndex,
	)

	if err != nil {
		s.logger.Error("could not decode bridge message event", "err", err)

		return err
	}

	if err := s.state.BridgeMessageStore.insertBridgeMessageEvent(event); err != nil {
		s.logger.Error("could not save state sync event to boltDb", "err", err)

		return err
	}

	if err := s.buildBridgeBatch(nil, event.DestinationChainID.Uint64()); err != nil {
		// we don't return an error here. If state sync event is inserted in db,
		// we will just try to build a commitment on next block or next event arrival
		s.logger.Error("could not build a commitment on arrival of new state sync", "err", err, "bridgeMessageID", event.ID)
	}

	return nil
}

// GetVotedBridgeBatch returns a commitment to be submitted if there is a pending commitment with quorum
func (s *bridgeEventManager) GetVotedBridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var largestBridgeBatch *BridgeBatchSigned

	// we start from the end, since last pending commitment is the largest one
	for i := len(s.pendingBridgeBatches) - 1; i >= 0; i-- {
		commitment := s.pendingBridgeBatches[i]
		aggregatedSignature, publicKeys, err := s.getAggSignatureForBridgeBatchMessage(blockNumber, commitment)

		if err != nil {
			if errors.Is(err, errQuorumNotReached) {
				// a valid case, commitment has no quorum, we should not return an error
				s.logger.Debug("can not submit a commitment, quorum not reached",
					"from", commitment.BridgeMessageBatch.SourceChainID.Uint64(),
					"to", commitment.BridgeMessageBatch.DestinationChainID.Uint64())

				continue
			}

			return nil, err
		}

		largestBridgeBatch = &BridgeBatchSigned{
			MessageBatch: commitment.BridgeMessageBatch,
			AggSignature: aggregatedSignature,
			PublicKeys:   publicKeys,
		}

		break
	}

	return largestBridgeBatch, nil
}

// getAggSignatureForBridgeBatchMessage checks if pending commitment has quorum,
// and if it does, aggregates the signatures
func (s *bridgeEventManager) getAggSignatureForBridgeBatchMessage(blockNumber uint64,
	pendingBridgeBatch *PendingBridgeBatch) (Signature, [][]byte, error) {
	validatorSet := s.validatorSet

	validatorAddrToIndex := make(map[string]int, validatorSet.Len())
	validatorsMetadata := validatorSet.Accounts()

	for i, validator := range validatorsMetadata {
		validatorAddrToIndex[validator.Address.String()] = i
	}

	bridgeBatchHash, err := pendingBridgeBatch.Hash()
	if err != nil {
		return Signature{}, nil, err
	}

	// get all the votes from the database for this commitment
	votes, err := s.state.BridgeMessageStore.getMessageVotes(
		pendingBridgeBatch.Epoch,
		bridgeBatchHash.Bytes(),
		pendingBridgeBatch.BridgeMessageBatch.DestinationChainID.Uint64())
	if err != nil {
		return Signature{}, nil, err
	}

	var signatures bls.Signatures

	publicKeys := make([][]byte, 0)
	bmap := bitmap.Bitmap{}
	signers := make(map[types.Address]struct{}, 0)

	for _, vote := range votes {
		index, exists := validatorAddrToIndex[vote.From]
		if !exists {
			continue // don't count this vote, because it does not belong to validator
		}

		signature, err := bls.UnmarshalSignature(vote.Signature)
		if err != nil {
			return Signature{}, nil, err
		}

		bmap.Set(uint64(index))

		signatures = append(signatures, signature)
		publicKeys = append(publicKeys, validatorsMetadata[index].BlsKey.Marshal())
		signers[types.StringToAddress(vote.From)] = struct{}{}
	}

	if !validatorSet.HasQuorum(blockNumber, signers) {
		return Signature{}, nil, errQuorumNotReached
	}

	aggregatedSignature, err := signatures.Aggregate().Marshal()
	if err != nil {
		return Signature{}, nil, err
	}

	result := Signature{
		AggregatedSignature: aggregatedSignature,
		Bitmap:              bmap,
	}

	return result, publicKeys, nil
}

// PostEpoch notifies the bridge event manager that an epoch has changed,
// so that it can discard any previous epoch bridge batch, and build a new one (since validator set changed)
func (s *bridgeEventManager) PostEpoch(req *PostEpochRequest, chainID uint64) error {
	s.lock.Lock()

	s.pendingBridgeBatches = nil
	s.validatorSet = req.ValidatorSet
	s.epoch = req.NewEpochID

	// build a new commitment at the end of the epoch
	nextCommittedIndex, err := req.SystemState.GetNextCommittedIndex(chainID)
	if err != nil {
		s.lock.Unlock()

		return err
	}

	s.nextBridgeEventIdIndex[chainID] = nextCommittedIndex

	s.lock.Unlock()

	return s.buildBridgeBatch(req.DBTx, chainID)
}

// PostBlock notifies state sync manager that a block was finalized,.
// Additionally, it will remove any processed bridge message events.
func (s *bridgeEventManager) PostBlock(req *PostBlockRequest) error {
	bridgeBatch, err := getBridgeBatchSignedTx(req.FullBlock.Block.Transactions)
	if err != nil {
		return err
	}

	// no bridge batch message -> this is not end of sprint block
	if bridgeBatch == nil {
		return nil
	}

	if err := s.state.BridgeMessageStore.insertBridgeBatchMessage(bridgeBatch, req.DBTx); err != nil {
		return fmt.Errorf("insert commitment message error: %w", err)
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	length := len(bridgeBatch.MessageBatch.Messages)
	// update the nextBridgeEventIdIndex since a bridge batch was submitted
	s.nextBridgeEventIdIndex[bridgeBatch.MessageBatch.DestinationChainID.Uint64()] =
		bridgeBatch.MessageBatch.Messages[length-1].ID.Uint64() + 1
	// commitment was submitted, so discard what we have in memory, so we can build a new one
	s.pendingBridgeBatches = nil

	return nil
}

// buildBridgeBatch builds a new bridge batch, signs it and gossips its vote for it
func (s *bridgeEventManager) buildBridgeBatch(dbTx *bolt.Tx, chainId uint64) error {
	if !s.runtime.IsActiveValidator() {
		// don't build commitment if not a validator
		return nil
	}

	s.lock.RLock()

	// Since lock is reduced grab original values into local variables in order to keep them
	epoch := s.epoch
	bridgeMessageEvents, err := s.state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(
		s.nextBridgeEventIdIndex[chainId],
		s.nextBridgeEventIdIndex[chainId]+s.config.maxCommitmentSize-1,
		dbTx,
		chainId)
	if err != nil && !errors.Is(err, errNotEnoughBridgeEvents) {
		s.lock.RUnlock()
		return fmt.Errorf("failed to get state sync events for commitment. Error: %w", err)
	}

	if len(bridgeMessageEvents) == 0 {
		// there are no bridge message events
		s.lock.RUnlock()
		return nil
	}

	if len(s.pendingBridgeBatches) > 0 &&
		s.pendingBridgeBatches[len(s.pendingBridgeBatches)-1].
			BridgeMessageBatch.Messages[0].ID.
			Cmp(bridgeMessageEvents[len(bridgeMessageEvents)-1].ID) >= 0 {
		// already built a bridge batch of this size which is pending to be submitted
		s.lock.RUnlock()
		return nil
	}

	s.lock.RUnlock()

	pendingBridgeBatch, err := NewPendingBridgeBatch(epoch, bridgeMessageEvents)
	if err != nil {
		return err
	}

	batchBytes, err := pendingBridgeBatch.BridgeMessageBatch.EncodeAbi()
	if err != nil {
		return err
	}

	signature, err := s.config.key.SignWithDomain(batchBytes, signer.DomainStateReceiver)
	if err != nil {
		return fmt.Errorf("failed to sign commitment message. Error: %w", err)
	}

	sig := &MessageSignature{
		From:      s.config.key.String(),
		Signature: signature,
	}

	destinationChainId := pendingBridgeBatch.BridgeMessageBatch.DestinationChainID.Uint64()

	if _, err = s.state.BridgeMessageStore.insertMessageVote(
		epoch,
		batchBytes,
		sig,
		dbTx,
		destinationChainId); err != nil {
		return fmt.Errorf(
			"failed to insert signature for message batch to the state. Error: %w",
			err,
		)
	}

	// gossip message
	s.multicast(&TransportMessage{
		BatchByte:          batchBytes,
		Signature:          signature,
		From:               s.config.key.String(),
		EpochNumber:        epoch,
		DestinationChainID: destinationChainId,
	})

	length := len(pendingBridgeBatch.BridgeMessageBatch.Messages)

	if length > 0 {

		s.logger.Debug(
			"[buildCommitment] Built commitment",
			"from", pendingBridgeBatch.BridgeMessageBatch.Messages[0].ID.Uint64(),
			"to", pendingBridgeBatch.BridgeMessageBatch.Messages[length-1].ID.Uint64(),
		)
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	s.pendingBridgeBatches = append(s.pendingBridgeBatches, pendingBridgeBatch)

	return nil
}

// multicast publishes given message to the rest of the network
func (s *bridgeEventManager) multicast(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		s.logger.Warn("failed to marshal bridge message", "err", err)

		return
	}

	err = s.config.topic.Publish(&polybftProto.TransportMessage{Data: data})
	if err != nil {
		s.logger.Warn("failed to gossip bridge message", "err", err)
	}
}

// EventSubscriber implementation

// GetLogFilters returns a map of log filters for getting desired events,
// where the key is the address of contract that emits desired events,
// and the value is a slice of signatures of events we want to get.
// This function is the implementation of EventSubscriber interface
func (s *bridgeEventManager) GetLogFilters() map[types.Address][]types.Hash {
	var stateSyncResultEvent contractsapi.StateSyncResultEvent

	return map[types.Address][]types.Hash{
		contracts.StateReceiverContract: {types.Hash(stateSyncResultEvent.Sig())},
	}
}

// ProcessLog is the implementation of EventSubscriber interface,
// used to handle a log defined in GetLogFilters, provided by event provider
func (s *bridgeEventManager) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	var bridgeMessageResultEvent *contractsapi.BridgeMessageResultEvent

	doesMatch, err := bridgeMessageResultEvent.ParseLog(log)
	if err != nil {
		return err
	}

	if !doesMatch {
		return nil
	}

	return s.state.BridgeMessageStore.removeBridgeEventsAndProofs(bridgeMessageResultEvent)
}
