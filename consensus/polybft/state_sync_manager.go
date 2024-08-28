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
	Commitment(blockNumber uint64) (*CommitmentMessageSigned, error)
	PostBlock(req *PostBlockRequest) error
	PostEpoch(req *PostEpochRequest, destinationChainID uint64) error
}

var _ StateSyncManager = (*dummyStateSyncManager)(nil)

// dummyStateSyncManager is used when bridge is not enabled
type dummyStateSyncManager struct{}

func (d *dummyStateSyncManager) Init() error                      { return nil }
func (d *dummyStateSyncManager) AddLog(eventLog *ethgo.Log) error { return nil }
func (d *dummyStateSyncManager) Commitment(blockNumber uint64) (*CommitmentMessageSigned, error) {
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

var _ StateSyncManager = (*stateSyncManager)(nil)

// stateSyncManager is a struct that manages the workflow of
// saving and querying state sync events, and creating, and submitting new commitments
type stateSyncManager struct {
	logger hclog.Logger
	state  *State

	config *stateSyncConfig

	// per epoch fields
	lock               sync.RWMutex
	pendingCommitments []*PendingCommitment
	validatorSet       validator.ValidatorSet
	epoch              uint64
	nextCommittedIndex map[uint64]uint64

	runtime Runtime
}

// topic is an interface for p2p message gossiping
type topic interface {
	Publish(obj proto.Message) error
	Subscribe(handler func(obj interface{}, from peer.ID)) error
}

// newStateSyncManager creates a new instance of state sync manager
func newStateSyncManager(logger hclog.Logger, state *State, config *stateSyncConfig,
	runtime Runtime) *stateSyncManager {
	return &stateSyncManager{
		logger:             logger,
		state:              state,
		config:             config,
		runtime:            runtime,
		nextCommittedIndex: make(map[uint64]uint64),
	}
}

// Init subscribes to bridge topics (getting votes) and start the event tracker routine
func (s *stateSyncManager) Init() error {
	if err := s.initTransport(); err != nil {
		return fmt.Errorf("failed to initialize state sync transport layer. Error: %w", err)
	}

	return nil
}

// initTransport subscribes to bridge topics (getting votes for commitments)
func (s *stateSyncManager) initTransport() error {
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
func (s *stateSyncManager) saveVote(msg *TransportMessage) error {
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

	numSignatures, err := s.state.BridgeMessageStore.insertMessageVote(msg.EpochNumber, msg.BatchByte, msgVote, nil, msg.DestinationChainID)
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
func (s *stateSyncManager) verifyVoteSignature(valSet validator.ValidatorSet, signerAddr types.Address,
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

// AddLog saves the received log from event tracker if it matches a state sync event ABI
func (s *stateSyncManager) AddLog(eventLog *ethgo.Log) error {
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

	if err := s.buildCommitment(nil, event.DestinationChainID.Uint64()); err != nil {
		// we don't return an error here. If state sync event is inserted in db,
		// we will just try to build a commitment on next block or next event arrival
		s.logger.Error("could not build a commitment on arrival of new state sync", "err", err, "bridgeMessageID", event.ID)
	}

	return nil
}

// Commitment returns a commitment to be submitted if there is a pending commitment with quorum
func (s *stateSyncManager) Commitment(blockNumber uint64) (*CommitmentMessageSigned, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var largestCommitment *CommitmentMessageSigned

	// we start from the end, since last pending commitment is the largest one
	for i := len(s.pendingCommitments) - 1; i >= 0; i-- {
		commitment := s.pendingCommitments[i]
		aggregatedSignature, publicKeys, err := s.getAggSignatureForCommitmentMessage(blockNumber, commitment)

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

		largestCommitment = &CommitmentMessageSigned{
			MessageBatch: commitment.BridgeMessageBatch,
			AggSignature: aggregatedSignature,
			PublicKeys:   publicKeys,
		}

		break
	}

	return largestCommitment, nil
}

// getAggSignatureForCommitmentMessage checks if pending commitment has quorum,
// and if it does, aggregates the signatures
func (s *stateSyncManager) getAggSignatureForCommitmentMessage(blockNumber uint64,
	commitment *PendingCommitment) (Signature, [][]byte, error) {
	validatorSet := s.validatorSet

	validatorAddrToIndex := make(map[string]int, validatorSet.Len())
	validatorsMetadata := validatorSet.Accounts()

	for i, validator := range validatorsMetadata {
		validatorAddrToIndex[validator.Address.String()] = i
	}

	commitmentHash, err := commitment.Hash()
	if err != nil {
		return Signature{}, nil, err
	}

	// get all the votes from the database for this commitment
	votes, err := s.state.BridgeMessageStore.getMessageVotes(commitment.Epoch, commitmentHash.Bytes(), commitment.BridgeMessageBatch.DestinationChainID.Uint64())
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

// PostEpoch notifies the state sync manager that an epoch has changed,
// so that it can discard any previous epoch commitments, and build a new one (since validator set changed)
func (s *stateSyncManager) PostEpoch(req *PostEpochRequest, chainID uint64) error {
	s.lock.Lock()

	s.pendingCommitments = nil
	s.validatorSet = req.ValidatorSet
	s.epoch = req.NewEpochID

	// build a new commitment at the end of the epoch
	nextCommittedIndex, err := req.SystemState.GetNextCommittedIndex(chainID)
	if err != nil {
		s.lock.Unlock()

		return err
	}

	s.nextCommittedIndex[chainID] = nextCommittedIndex

	s.lock.Unlock()

	return s.buildCommitment(req.DBTx, chainID)
}

// PostBlock notifies state sync manager that a block was finalized,
// so that it can build state sync proofs if a block has a commitment submission transaction.
// Additionally, it will remove any processed state sync events and their proofs from the store.
func (s *stateSyncManager) PostBlock(req *PostBlockRequest) error {
	commitment, err := getCommitmentMessageSignedTx(req.FullBlock.Block.Transactions)
	if err != nil {
		return err
	}

	// no commitment message -> this is not end of sprint block
	if commitment == nil {
		return nil
	}

	if err := s.state.BridgeMessageStore.insertCommitmentMessage(commitment, req.DBTx); err != nil {
		return fmt.Errorf("insert commitment message error: %w", err)
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	length := len(commitment.MessageBatch.Messages)
	// update the nextCommittedIndex since a commitment was submitted
	s.nextCommittedIndex[commitment.MessageBatch.DestinationChainID.Uint64()] = commitment.MessageBatch.Messages[length-1].ID.Uint64() + 1
	// commitment was submitted, so discard what we have in memory, so we can build a new one
	s.pendingCommitments = nil

	return nil
}

// buildCommitment builds a new commitment, signs it and gossips its vote for it
func (s *stateSyncManager) buildCommitment(dbTx *bolt.Tx, chainId uint64) error {
	if !s.runtime.IsActiveValidator() {
		// don't build commitment if not a validator
		return nil
	}

	s.lock.RLock()

	// Since lock is reduced grab original values into local variables in order to keep them
	epoch := s.epoch
	bridgeMessageEvents, err := s.state.BridgeMessageStore.getBridgeMessageEventsForCommitment(s.nextCommittedIndex[chainId],
		s.nextCommittedIndex[chainId]+s.config.maxCommitmentSize-1, dbTx, chainId)
	if err != nil && !errors.Is(err, errNotEnoughStateSyncs) {
		s.lock.RUnlock()
		return fmt.Errorf("failed to get state sync events for commitment. Error: %w", err)
	}

	if len(bridgeMessageEvents) == 0 {
		// there are no state sync events
		s.lock.RUnlock()
		return nil
	}

	if len(s.pendingCommitments) > 0 &&
		s.pendingCommitments[len(s.pendingCommitments)-1].BridgeMessageBatch.Messages[0].ID.Cmp(bridgeMessageEvents[len(bridgeMessageEvents)-1].ID) >= 0 {
		// already built a commitment of this size which is pending to be submitted
		s.lock.RUnlock()
		return nil
	}

	s.lock.RUnlock()

	commitment, err := NewPendingCommitment(epoch, bridgeMessageEvents)
	if err != nil {
		return err
	}

	batchBytes, err := commitment.BridgeMessageBatch.EncodeAbi()
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

	destinationChainId := commitment.BridgeMessageBatch.DestinationChainID.Uint64()

	if _, err = s.state.BridgeMessageStore.insertMessageVote(epoch, batchBytes, sig, dbTx, destinationChainId); err != nil {
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

	length := len(commitment.BridgeMessageBatch.Messages)

	if length > 0 {

		s.logger.Debug(
			"[buildCommitment] Built commitment",
			"from", commitment.BridgeMessageBatch.Messages[0].ID.Uint64(),
			"to", commitment.BridgeMessageBatch.Messages[length-1].ID.Uint64(),
		)
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	s.pendingCommitments = append(s.pendingCommitments, commitment)

	return nil
}

// multicast publishes given message to the rest of the network
func (s *stateSyncManager) multicast(msg interface{}) {
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
func (s *stateSyncManager) GetLogFilters() map[types.Address][]types.Hash {
	var stateSyncResultEvent contractsapi.StateSyncResultEvent

	return map[types.Address][]types.Hash{
		contracts.StateReceiverContract: {types.Hash(stateSyncResultEvent.Sig())},
	}
}

// ProcessLog is the implementation of EventSubscriber interface,
// used to handle a log defined in GetLogFilters, provided by event provider
func (s *stateSyncManager) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	var bridgeMessageResultEvent *contractsapi.BridgeMessageResultEvent

	doesMatch, err := bridgeMessageResultEvent.ParseLog(log)
	if err != nil {
		return err
	}

	if !doesMatch {
		return nil
	}

	return s.state.BridgeMessageStore.removeStateSyncEventsAndProofs(bridgeMessageResultEvent)
}
