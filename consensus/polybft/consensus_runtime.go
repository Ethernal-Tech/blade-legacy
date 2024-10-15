package polybft

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xPolygon/go-ibft/messages"
	"github.com/0xPolygon/go-ibft/messages/proto"
	hcf "github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
	protobuf "google.golang.org/protobuf/proto"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bridge"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/epoch"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/governance"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
	polymetrics "github.com/0xPolygon/polygon-edge/consensus/polybft/metrics"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/oracle"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/proposer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/stake"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
)

const stateFileName = "consensusState.db"

var (
	// errNotAValidator represents "node is not a validator" error message
	errNotAValidator = errors.New("node is not a validator")
)

// epochMetadata is the static info for epoch currently being processed
type epochMetadata struct {
	// Number is the number of the epoch
	Number uint64

	FirstBlockInEpoch uint64

	// Validators is the set of validators for the epoch
	Validators validator.AccountSet

	// CurrentClientConfig is the current client configuration for current epoch
	// that is updated by governance proposals
	CurrentClientConfig *config.PolyBFT
}

type guardedDataDTO struct {
	// last built block header at the time of collecting data
	lastBuiltBlock *types.Header

	// epoch metadata at the time of collecting data
	epoch *epochMetadata

	// proposerSnapshot at the time of collecting data
	proposerSnapshot *proposer.ProposerSnapshot
}

// consensusRuntime is a struct that provides consensus runtime features like epoch, state and event management
type consensusRuntime struct {
	// config represents wrapper around required parameters which are received from the outside
	config *config.Runtime

	blockchain blockchain.Blockchain

	backend polytypes.Polybft

	txPool blockchain.TxPool

	// state is reference to the struct which encapsulates bridge events persistence logic
	state *state.State

	// fsm instance which is created for each `runSequence`
	fsm *fsm

	// lock is a lock to access 'epoch' and `lastBuiltBlock`
	lock sync.RWMutex

	// epoch is the metadata for the current epoch
	epoch *epochMetadata

	// lastBuiltBlock is the header of the last processed block
	lastBuiltBlock *types.Header

	// activeValidatorFlag indicates whether the given node is amongst currently active validator set
	activeValidatorFlag atomic.Bool

	// proposerCalculator is the object which manipulates with ProposerSnapshot
	proposerCalculator *proposer.ProposerCalculator

	// manager for handling validator stake change and updating validator set
	stakeManager stake.StakeManager

	// eventProvider is used for tracking events from the blockchain
	eventProvider *state.EventProvider

	// governanceManager is used for handling governance events gotten from proposals execution
	// also handles updating client configuration based on governance proposals
	governanceManager governance.GovernanceManager

	// oracles are all registered oracles
	oracles oracle.Oracles

	// logger instance
	logger hcf.Logger
}

// newConsensusRuntime creates and starts a new consensus runtime instance with event tracking
func newConsensusRuntime(log hcf.Logger, config *config.Runtime,
	st *state.State,
	backend polytypes.Polybft,
	blockchain blockchain.Blockchain,
	txPool blockchain.TxPool,
	bridgeTopic bridge.Topic,
) (*consensusRuntime, error) {
	dbTx, err := st.BeginDBTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("could not begin dbTx to init consensus runtime: %w", err)
	}

	defer dbTx.Rollback() //nolint:errcheck

	proposerCalculator, err := proposer.NewProposerCalculator(
		config, log.Named("proposer_calculator"),
		st, backend, blockchain, dbTx)
	if err != nil {
		return nil, fmt.Errorf("failed to create consensus runtime, error while creating proposer calculator %w", err)
	}

	runtime := &consensusRuntime{
		state:              st,
		config:             config,
		lastBuiltBlock:     blockchain.CurrentHeader(),
		proposerCalculator: proposerCalculator,
		logger:             log.Named("consensus_runtime"),
		eventProvider:      state.NewEventProvider(blockchain),
		backend:            backend,
		blockchain:         blockchain,
		txPool:             txPool,
	}

	runtime.oracles = append(runtime.oracles, proposerCalculator)
	runtime.oracles = append(runtime.oracles, epoch.NewEpochManager(backend, blockchain))

	bridge, err := bridge.NewBridge(
		runtime,
		runtime.state,
		runtime.config,
		bridgeTopic,
		runtime.eventProvider,
		runtime.blockchain,
		log.Named("bridge"), dbTx)
	if err != nil {
		return nil, err
	}

	runtime.oracles = append(runtime.oracles, bridge)

	if err := runtime.initStakeManager(log, dbTx); err != nil {
		return nil, err
	}

	if err := runtime.initGovernanceManager(log, dbTx); err != nil {
		return nil, err
	}

	// we need to call restart epoch on runtime to initialize epoch state
	runtime.epoch, err = runtime.restartEpoch(runtime.lastBuiltBlock, dbTx)
	if err != nil {
		return nil, fmt.Errorf("consensus runtime creation - restart epoch failed: %w", err)
	}

	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("could not commit db tx to init consensus runtime: %w", err)
	}

	return runtime, nil
}

// close is used to tear down allocated resources
func (c *consensusRuntime) close() {
	c.oracles.Close()
}

// initStakeManager initializes stake manager
func (c *consensusRuntime) initStakeManager(logger hcf.Logger, dbTx *bolt.Tx) error {
	var err error

	c.stakeManager, err = stake.NewStakeManager(
		logger.Named("stake-manager"),
		c.state,
		contracts.StakeManagerContract,
		c.blockchain,
		c.backend,
		dbTx,
	)

	c.eventProvider.Subscribe(c.stakeManager)
	c.oracles = append(c.oracles, c.stakeManager)

	return err
}

// initGovernanceManager initializes governance manager
func (c *consensusRuntime) initGovernanceManager(logger hcf.Logger, dbTx *bolt.Tx) error {
	governanceManager, err := governance.NewGovernanceManager(
		c.config.ChainParams,
		logger.Named("governance-manager"),
		c.state,
		c.blockchain,
		dbTx,
	)

	if err != nil {
		return err
	}

	c.governanceManager = governanceManager
	c.eventProvider.Subscribe(c.governanceManager)

	c.oracles = append(c.oracles, c.governanceManager)

	return nil
}

// getGuardedData returns last build block, proposer snapshot and current epochMetadata in a thread-safe manner.
func (c *consensusRuntime) getGuardedData() (guardedDataDTO, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	lastBuiltBlock := c.lastBuiltBlock.Copy()
	epoch := new(epochMetadata)
	*epoch = *c.epoch // shallow copy, don't need to make validators copy because AccountSet is immutable
	proposerSnapshot, ok := c.proposerCalculator.GetSnapshot()

	if !ok {
		return guardedDataDTO{}, errors.New("cannot collect shared data, snapshot is empty")
	}

	return guardedDataDTO{
		epoch:            epoch,
		lastBuiltBlock:   lastBuiltBlock,
		proposerSnapshot: proposerSnapshot,
	}, nil
}

func (c *consensusRuntime) IsBridgeEnabled() bool {
	// this is enough to check, because bridge config is not something
	// that can be changed through governance
	return c.config.GenesisConfig.IsBridgeEnabled()
}

// OnBlockInserted is called whenever fsm or syncer inserts new block
func (c *consensusRuntime) OnBlockInserted(fullBlock *types.FullBlock) {
	startTime := time.Now().UTC()

	c.lock.Lock()
	defer c.lock.Unlock()

	if c.lastBuiltBlock != nil && c.lastBuiltBlock.Number >= fullBlock.Block.Number() {
		c.logger.Debug("on block inserted already handled",
			"current", c.lastBuiltBlock.Number, "block", fullBlock.Block.Number())

		return
	}

	if err := polymetrics.UpdateBlockMetrics(fullBlock.Block, c.lastBuiltBlock); err != nil {
		c.logger.Error("failed to update block metrics", "error", err)
	}

	// after the block has been written we reset the txpool so that the old transactions are removed
	c.txPool.ResetWithBlock(fullBlock.Block)

	var (
		epoch = c.epoch
		err   error
		// calculation of epoch and sprint end does not consider slashing currently

		isEndOfEpoch = c.isFixedSizeOfEpochMet(fullBlock.Block.Header.Number, epoch)
	)

	// begin DB transaction
	dbTx, err := c.state.BeginDBTransaction(true)
	if err != nil {
		c.logger.Error("failed to begin db transaction on block finalization",
			"block", fullBlock.Block.Number(), "err", err)

		return
	}

	defer dbTx.Rollback() //nolint:errcheck

	lastProcessedEventsBlock, err := c.state.GetLastProcessedEventsBlock(dbTx)
	if err != nil {
		c.logger.Error("failed to get last processed events block on block finalization",
			"block", fullBlock.Block.Number(), "err", err)

		return
	}

	if err := c.eventProvider.GetEventsFromBlocks(lastProcessedEventsBlock, fullBlock, dbTx); err != nil {
		c.logger.Error("failed to process events on block finalization", "block", fullBlock.Block.Number(), "err", err)

		return
	}

	postBlock := &polytypes.PostBlockRequest{
		FullBlock:           fullBlock,
		Epoch:               epoch.Number,
		IsEpochEndingBlock:  isEndOfEpoch,
		DBTx:                dbTx,
		CurrentClientConfig: epoch.CurrentClientConfig,
		Forks:               c.config.Forks,
	}

	if err := c.oracles.PostBlock(postBlock); err != nil {
		c.logger.Error("failed to post block in oracles", "err", err)
	}

	if isEndOfEpoch {
		if epoch, err = c.restartEpoch(fullBlock.Block.Header, dbTx); err != nil {
			c.logger.Error("failed to restart epoch after block inserted", "error", err)

			return
		}
	}

	if err := c.state.InsertLastProcessedEventsBlock(fullBlock.Block.Number(), dbTx); err != nil {
		c.logger.Error("failed to update the last processed events block in db", "error", err)

		return
	}

	// commit DB transaction
	if err := dbTx.Commit(); err != nil {
		c.logger.Error("failed to commit transaction on PostBlock",
			"block", fullBlock.Block.Number(), "error", err)

		return
	}

	// finally update runtime state (lastBuiltBlock, epoch, proposerSnapshot)
	c.epoch = epoch
	c.lastBuiltBlock = fullBlock.Block.Header

	endTime := time.Now().UTC()

	c.logger.Debug("OnBlockInserted finished", "elapsedTime", endTime.Sub(startTime),
		"epoch", epoch.Number, "block", fullBlock.Block.Number())
}

// FSM creates a new instance of fsm
func (c *consensusRuntime) FSM() error {
	sharedData, err := c.getGuardedData()
	if err != nil {
		return fmt.Errorf("cannot create fsm: %w", err)
	}

	parent, epoch, proposerSnapshot := sharedData.lastBuiltBlock, sharedData.epoch, sharedData.proposerSnapshot

	if !epoch.Validators.ContainsNodeID(c.config.Key.String()) {
		return errNotAValidator
	}

	blockBuilder, err := c.blockchain.NewBlockBuilder(
		parent,
		c.config.Key.Address(),
		c.txPool,
		epoch.CurrentClientConfig.BlockTime.Duration,
		c.logger,
	)

	if err != nil {
		return fmt.Errorf("cannot create block builder for fsm: %w", err)
	}

	pendingBlockNumber := parent.Number + 1
	// calculation of epoch and sprint end does not consider slashing currently
	isEndOfSprint := c.isFixedSizeOfSprintMet(pendingBlockNumber, epoch)
	isEndOfEpoch := c.isFixedSizeOfEpochMet(pendingBlockNumber, epoch)
	isFirstBlockOfEpoch := pendingBlockNumber == epoch.FirstBlockInEpoch

	valSet := validator.NewValidatorSet(epoch.Validators, c.logger)

	ff := &fsm{
		config:           epoch.CurrentClientConfig,
		blockchain:       c.blockchain,
		polybftBackend:   c.backend,
		blockBuilder:     blockBuilder,
		proposerSnapshot: proposerSnapshot,
		logger:           c.logger.Named("fsm"),
		oracles:          c.oracles,
	}

	var newValidatorsDelta *validator.ValidatorSetDelta
	if isEndOfEpoch {
		newValidatorsDelta, err = c.stakeManager.UpdateValidatorSet(epoch.Number,
			epoch.CurrentClientConfig.MaxValidatorSetSize, epoch.Validators.Copy())
		if err != nil {
			return fmt.Errorf("cannot update validator set on epoch ending: %w", err)
		}
	}

	ff.blockInfo = oracle.NewBlockInfo{
		ParentBlock:              parent,
		IsEndOfEpoch:             isEndOfEpoch,
		IsEndOfSprint:            isEndOfSprint,
		IsFirstBlockOfEpoch:      isFirstBlockOfEpoch,
		FirstBlockInEpoch:        epoch.FirstBlockInEpoch,
		CurrentEpoch:             epoch.Number,
		EpochSize:                epoch.CurrentClientConfig.EpochSize,
		CurrentEpochValidatorSet: valSet,
		ValidatorSetDelta:        newValidatorsDelta,
	}

	c.logger.Info(
		"[FSM built]",
		"epoch", epoch.Number,
		"endOfEpoch", isEndOfEpoch,
		"endOfSprint", isEndOfSprint,
	)

	c.lock.Lock()
	c.fsm = ff
	c.lock.Unlock()

	return nil
}

// restartEpoch resets the previously run epoch and moves to the next one
// returns *epochMetadata different from nil if the lastEpoch is not the current one and everything was successful
func (c *consensusRuntime) restartEpoch(header *types.Header, dbTx *bolt.Tx) (*epochMetadata, error) {
	lastEpoch := c.epoch

	systemState, err := c.getSystemState(header)
	if err != nil {
		return nil, fmt.Errorf("get system state: %w", err)
	}

	epochNumber, err := systemState.GetEpoch()
	if err != nil {
		return nil, fmt.Errorf("get epoch: %w", err)
	}

	if lastEpoch != nil {
		// Epoch might be already in memory, if its the same number do nothing -> just return provided last one
		// Otherwise, reset the epoch metadata and restart the async services
		if lastEpoch.Number == epochNumber {
			return lastEpoch, nil
		}
	}

	validatorSet, err := c.backend.GetValidatorsWithTx(header.Number, nil, dbTx)
	if err != nil {
		return nil, fmt.Errorf("restart epoch - cannot get validators: %w", err)
	}

	polymetrics.UpdateEpochMetrics(epochNumber, len(validatorSet))

	firstBlockInEpoch, err := c.getFirstBlockOfEpoch(epochNumber, header)
	if err != nil {
		return nil, err
	}

	c.logger.Info(
		"restartEpoch",
		"block number", header.Number,
		"epoch", epochNumber,
		"validators", validatorSet.Len(),
		"firstBlockInEpoch", firstBlockInEpoch,
	)

	reqObj := &polytypes.PostEpochRequest{
		SystemState:       systemState,
		NewEpochID:        epochNumber,
		FirstBlockOfEpoch: firstBlockInEpoch,
		ValidatorSet:      validator.NewValidatorSet(validatorSet, c.logger),
		DBTx:              dbTx,
		Forks:             c.config.Forks,
	}

	if err := c.oracles.PostEpoch(reqObj); err != nil {
		return nil, err
	}

	currentParams, err := c.governanceManager.GetClientConfig(dbTx)
	if err != nil {
		return nil, err
	}

	currentPolyConfig, err := config.GetPolyBFTConfig(currentParams)
	if err != nil {
		return nil, err
	}

	c.backend.SetBlockTime(currentPolyConfig.BlockTime.Duration)

	return &epochMetadata{
		Number:              epochNumber,
		Validators:          validatorSet,
		FirstBlockInEpoch:   firstBlockInEpoch,
		CurrentClientConfig: &currentPolyConfig,
	}, nil
}

// setIsActiveValidator updates the activeValidatorFlag field
func (c *consensusRuntime) setIsActiveValidator(isActiveValidator bool) {
	c.activeValidatorFlag.Store(isActiveValidator)
}

// isActiveValidator indicates if node is in validator set or not
func (c *consensusRuntime) IsActiveValidator() bool {
	return c.activeValidatorFlag.Load()
}

// isFixedSizeOfEpochMet checks if epoch reached its end that was configured by its default size
// this is only true if no slashing occurred in the given epoch
func (c *consensusRuntime) isFixedSizeOfEpochMet(blockNumber uint64, epoch *epochMetadata) bool {
	return epoch.FirstBlockInEpoch+epoch.CurrentClientConfig.EpochSize-1 == blockNumber
}

// isFixedSizeOfSprintMet checks if an end of an sprint is reached with the current block
func (c *consensusRuntime) isFixedSizeOfSprintMet(blockNumber uint64, epoch *epochMetadata) bool {
	return (blockNumber-epoch.FirstBlockInEpoch+1)%epoch.CurrentClientConfig.SprintSize == 0
}

// getSystemState builds SystemState instance for the most current block header
func (c *consensusRuntime) getSystemState(header *types.Header) (systemstate.SystemState, error) {
	provider, err := c.blockchain.GetStateProviderForBlock(header)
	if err != nil {
		return nil, err
	}

	return c.blockchain.GetSystemState(provider), nil
}

func (c *consensusRuntime) IsValidProposal(rawProposal []byte) bool {
	if err := c.fsm.Validate(rawProposal); err != nil {
		c.logger.Error("failed to validate proposal", "error", err)

		return false
	}

	return true
}

func (c *consensusRuntime) IsValidValidator(msg *proto.IbftMessage) bool {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.fsm == nil {
		c.logger.Warn("unable to validate IBFT message sender, because FSM is not initialized")

		return false
	}

	if err := c.fsm.ValidateSender(msg); err != nil {
		c.logger.Error("invalid IBFT message received", "error", err)

		return false
	}

	return true
}

func (c *consensusRuntime) IsProposer(id []byte, height, round uint64) bool {
	c.lock.RLock()
	defer c.lock.RUnlock()

	nextProposer, err := c.fsm.proposerSnapshot.CalcProposer(round, height)
	if err != nil {
		c.logger.Error("cannot calculate proposer", "error", err)

		return false
	}

	c.logger.Info("Proposer calculated", "height", height, "round", round, "address", nextProposer)

	return bytes.Equal(id, nextProposer[:])
}

func (c *consensusRuntime) IsValidProposalHash(proposal *proto.Proposal, hash []byte) bool {
	if len(proposal.RawProposal) == 0 {
		c.logger.Error("proposal hash is not valid because proposal is empty")

		return false
	}

	block := types.Block{}
	if err := block.UnmarshalRLP(proposal.RawProposal); err != nil {
		c.logger.Error("unable to unmarshal proposal", "error", err)

		return false
	}

	extra, err := polytypes.GetIbftExtra(block.Header.ExtraData)
	if err != nil {
		c.logger.Error("failed to retrieve extra", "block number", block.Number(), "error", err)

		return false
	}

	proposalHash, err := extra.BlockMetaData.Hash(block.Hash())
	if err != nil {
		c.logger.Error("failed to calculate proposal hash", "block number", block.Number(), "error", err)

		return false
	}

	return bytes.Equal(proposalHash.Bytes(), hash)
}

func (c *consensusRuntime) IsValidCommittedSeal(proposalHash []byte, committedSeal *messages.CommittedSeal) bool {
	err := c.fsm.ValidateCommit(committedSeal.Signer, committedSeal.Signature, proposalHash)
	if err != nil {
		c.logger.Info("Invalid committed seal", "error", err)

		return false
	}

	return true
}

func (c *consensusRuntime) BuildProposal(view *proto.View) []byte {
	sharedData, err := c.getGuardedData()
	if err != nil {
		c.logger.Error("unable to build proposal", "error", err)

		return nil
	}

	if sharedData.lastBuiltBlock.Number+1 != view.Height {
		c.logger.Error("unable to build proposal, due to lack of parent block",
			"parent height", sharedData.lastBuiltBlock.Number, "current height", view.Height)

		return nil
	}

	proposal, err := c.fsm.BuildProposal(view.Round)
	if err != nil {
		c.logger.Error("unable to build proposal", "blockNumber", view, "error", err)

		return nil
	}

	return proposal
}

// InsertProposal inserts a proposal with the specified committed seals
func (c *consensusRuntime) InsertProposal(proposal *proto.Proposal, committedSeals []*messages.CommittedSeal) {
	fsm := c.fsm

	fullBlock, err := fsm.Insert(proposal.RawProposal, committedSeals)
	if err != nil {
		c.logger.Error("cannot insert proposal", "error", err)

		return
	}

	c.OnBlockInserted(fullBlock)
}

// ID return ID (address actually) of the current node
func (c *consensusRuntime) ID() []byte {
	return c.config.Key.Address().Bytes()
}

// GetVotingPowers returns map of validators addresses and their voting powers for the specified height.
func (c *consensusRuntime) GetVotingPowers(height uint64) (map[string]*big.Int, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.fsm == nil {
		return nil, errors.New("getting voting power failed - backend is not initialized")
	} else if c.fsm.Height() != height {
		return nil, fmt.Errorf("getting voting power failed - backend is not initialized for height %d, fsm height %d",
			height, c.fsm.Height())
	}

	return c.fsm.blockInfo.CurrentEpochValidatorSet.GetVotingPowers(), nil
}

// BuildPrePrepareMessage builds a PREPREPARE message based on the passed in proposal
func (c *consensusRuntime) BuildPrePrepareMessage(
	rawProposal []byte,
	certificate *proto.RoundChangeCertificate,
	view *proto.View,
) *proto.IbftMessage {
	if len(rawProposal) == 0 {
		c.logger.Error("can not build pre-prepare message, since proposal is empty")

		return nil
	}

	block := types.Block{}
	if err := block.UnmarshalRLP(rawProposal); err != nil {
		c.logger.Error(fmt.Sprintf("cannot unmarshal RLP: %s", err))

		return nil
	}

	extra, err := polytypes.GetIbftExtra(block.Header.ExtraData)
	if err != nil {
		c.logger.Error("failed to retrieve extra for block %d: %w", block.Number(), err)

		return nil
	}

	proposalHash, err := extra.BlockMetaData.Hash(block.Hash())
	if err != nil {
		c.logger.Error("failed to calculate proposal hash", "block number", block.Number(), "error", err)

		return nil
	}

	proposal := &proto.Proposal{
		RawProposal: rawProposal,
		Round:       view.Round,
	}

	msg := proto.IbftMessage{
		View: view,
		From: c.ID(),
		Type: proto.MessageType_PREPREPARE,
		Payload: &proto.IbftMessage_PreprepareData{
			PreprepareData: &proto.PrePrepareMessage{
				Proposal:     proposal,
				ProposalHash: proposalHash.Bytes(),
				Certificate:  certificate,
			},
		},
	}

	message, err := c.config.Key.SignIBFTMessage(&msg)
	if err != nil {
		c.logger.Error("Cannot sign message", "error", err)

		return nil
	}

	return message
}

// BuildPrepareMessage builds a PREPARE message based on the passed in proposal
func (c *consensusRuntime) BuildPrepareMessage(proposalHash []byte, view *proto.View) *proto.IbftMessage {
	msg := proto.IbftMessage{
		View: view,
		From: c.ID(),
		Type: proto.MessageType_PREPARE,
		Payload: &proto.IbftMessage_PrepareData{
			PrepareData: &proto.PrepareMessage{
				ProposalHash: proposalHash,
			},
		},
	}

	message, err := c.config.Key.SignIBFTMessage(&msg)
	if err != nil {
		c.logger.Error("Cannot sign message.", "error", err)

		return nil
	}

	c.logger.Debug("Prepare message built", "blockNumber", view.Height, "round", view.Round)

	return message
}

// BuildCommitMessage builds a COMMIT message based on the passed in proposal
func (c *consensusRuntime) BuildCommitMessage(proposalHash []byte, view *proto.View) *proto.IbftMessage {
	committedSeal, err := c.config.Key.SignWithDomain(proposalHash, signer.DomainBridge)
	if err != nil {
		c.logger.Error("Cannot create committed seal message.", "error", err)

		return nil
	}

	msg := proto.IbftMessage{
		View: view,
		From: c.ID(),
		Type: proto.MessageType_COMMIT,
		Payload: &proto.IbftMessage_CommitData{
			CommitData: &proto.CommitMessage{
				ProposalHash:  proposalHash,
				CommittedSeal: committedSeal,
			},
		},
	}

	message, err := c.config.Key.SignIBFTMessage(&msg)
	if err != nil {
		c.logger.Error("Cannot sign message", "Error", err)

		return nil
	}

	return message
}

// RoundStarts represents the round start callback
func (c *consensusRuntime) RoundStarts(view *proto.View) error {
	c.logger.Info("RoundStarts", "height", view.Height, "round", view.Round)

	if view.Round > 0 {
		c.txPool.ReinsertProposed()
	} else {
		c.txPool.ClearProposed()
	}

	return nil
}

// SequenceCancelled represents sequence cancelled callback
func (c *consensusRuntime) SequenceCancelled(view *proto.View) error {
	c.logger.Info("SequenceCancelled", "height", view.Height, "round", view.Round)
	c.txPool.ReinsertProposed()

	return nil
}

// BuildRoundChangeMessage builds a ROUND_CHANGE message based on the passed in proposal
func (c *consensusRuntime) BuildRoundChangeMessage(
	proposal *proto.Proposal,
	certificate *proto.PreparedCertificate,
	view *proto.View,
) *proto.IbftMessage {
	msg := proto.IbftMessage{
		View: view,
		From: c.ID(),
		Type: proto.MessageType_ROUND_CHANGE,
		Payload: &proto.IbftMessage_RoundChangeData{
			RoundChangeData: &proto.RoundChangeMessage{
				LastPreparedProposal:      proposal,
				LatestPreparedCertificate: certificate,
			}},
	}

	signedMsg, err := c.config.Key.SignIBFTMessage(&msg)
	if err != nil {
		c.logger.Error("Cannot sign message", "Error", err)

		return nil
	}

	c.logRoundChangeMessage(view, proposal, certificate, &msg, signedMsg)

	return signedMsg
}

// getFirstBlockOfEpoch returns the first block of epoch in which provided header resides
func (c *consensusRuntime) getFirstBlockOfEpoch(epochNumber uint64, latestHeader *types.Header) (uint64, error) {
	if latestHeader.Number == 0 {
		// if we are starting the chain, we know that the first block is block 1
		return 1, nil
	}

	blockHeader := latestHeader

	blockExtra, err := polytypes.GetIbftExtra(latestHeader.ExtraData)
	if err != nil {
		return 0, err
	}

	if epochNumber != blockExtra.BlockMetaData.EpochNumber {
		// its a regular epoch ending. No out of sync happened
		return latestHeader.Number + 1, nil
	}

	// node was out of sync, so we need to figure out what was the first block of the given epoch
	epoch := blockExtra.BlockMetaData.EpochNumber

	var firstBlockInEpoch uint64

	for blockExtra.BlockMetaData.EpochNumber == epoch {
		firstBlockInEpoch = blockHeader.Number
		blockHeader, blockExtra, err = helpers.GetBlockData(blockHeader.Number-1, c.blockchain)

		if err != nil {
			return 0, err
		}
	}

	return firstBlockInEpoch, nil
}

// getCurrentBlockTimeDrift returns current block time drift
func (c *consensusRuntime) getCurrentBlockTimeDrift() uint64 {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.epoch.CurrentClientConfig.BlockTimeDrift
}

// logRoundChangeMessage logs the size of the round change message
func (c *consensusRuntime) logRoundChangeMessage(
	view *proto.View,
	proposal *proto.Proposal,
	certificate *proto.PreparedCertificate,
	msg *proto.IbftMessage,
	signedMsg *proto.IbftMessage) {
	if !c.logger.IsDebug() {
		return
	}

	var (
		preparedMsgsLen   = 0
		isPreparedCertNil = certificate == nil

		rawCertificate            = make([]byte, 0)
		rawCertificateProposalMsg = make([]byte, 0)
		err                       error
	)

	if certificate != nil {
		preparedMsgsLen = len(certificate.PrepareMessages)

		rawCertificate, err = protobuf.Marshal(certificate)
		if err != nil {
			c.logger.Error("Cannot marshal prepared certificate", "error", err)
		}

		if certificate.ProposalMessage != nil {
			rawCertificateProposalMsg, err = protobuf.Marshal(certificate.ProposalMessage)
			if err != nil {
				c.logger.Error("Cannot marshal prepared certificate proposal message", "error", err)
			}
		}
	}

	rawProposal := make([]byte, 0)

	isProposalNil := proposal == nil
	if !isProposalNil {
		rawProposal = proposal.RawProposal
	}

	msgRaw, err := protobuf.Marshal(msg)
	if err != nil {
		c.logger.Error("Cannot marshal round change message", "error", err)
	}

	signedMsgRaw, err := protobuf.Marshal(signedMsg)
	if err != nil {
		c.logger.Error("Cannot marshal signed round change message", "error", err)
	}

	c.logger.Debug("RoundChange message built", "blockNumber", view.Height, "round", view.Round,
		"totalRoundMsgSize", common.ToMB(msgRaw),
		"signedRoundMsgSize", common.ToMB(signedMsgRaw),
		"isProposalNil", isProposalNil,
		"proposalSize", common.ToMB(rawProposal),
		"isPreparedCertNil", isPreparedCertNil,
		"numOfPrepareMsgs", preparedMsgsLen,
		"certificateSize", common.ToMB(rawCertificate),
		"certificateProposalSize", common.ToMB(rawCertificateProposalMsg))
}
