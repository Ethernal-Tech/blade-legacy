package polybft

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/0xPolygon/go-ibft/messages"
	"github.com/0xPolygon/go-ibft/messages/proto"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-metrics"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	polymetrics "github.com/0xPolygon/polygon-edge/consensus/polybft/metrics"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/oracle"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/proposer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
)

type blockBuilder interface {
	Reset() error
	WriteTx(*types.Transaction) error
	Fill()
	Build(func(h *types.Header)) (*types.FullBlock, error)
	GetState() *state.Transition
	Receipts() []*types.Receipt
}

var (
	errProposalDontMatch = errors.New("failed to insert proposal, because the validated proposal " +
		"is either nil or it does not match the received one")
	errValidatorSetDeltaMismatch           = errors.New("validator set delta mismatch")
	errValidatorsUpdateInNonEpochEnding    = errors.New("trying to update validator set in a non epoch ending block")
	errValidatorDeltaNilInEpochEndingBlock = errors.New("validator set delta is nil in epoch ending block")
)

type fsm struct {
	// PolyBFT consensus protocol configuration
	config *config.PolyBFT

	// blockchain implements methods for retrieving data from block chain
	blockchain blockchain.Blockchain

	// polybftBackend implements methods needed from the polybft
	polybftBackend polytypes.Polybft

	// proposerSnapshot keeps information about new proposer
	proposerSnapshot *proposer.ProposerSnapshot

	// blockBuilder is the block builder for proposers
	blockBuilder blockBuilder

	// logger instance
	logger hclog.Logger

	// target is the block being computed
	target *types.FullBlock

	// oracles is the collection of registered oracles
	oracles oracle.Oracles

	// blockInfo holds information about the new block
	blockInfo oracle.NewBlockInfo
}

// BuildProposal builds a proposal for the current round (used if proposer)
func (f *fsm) BuildProposal(currentRound uint64) ([]byte, error) {
	start := time.Now().UTC()
	defer metrics.SetGauge([]string{polymetrics.ConsensusMetricsPrefix, "block_building_time"},
		float32(time.Now().UTC().Sub(start).Seconds()))

	parent := f.blockInfo.ParentBlock

	extraParent, err := polytypes.GetIbftExtra(parent.ExtraData)
	if err != nil {
		return nil, err
	}

	extra := &polytypes.Extra{Parent: extraParent.Committed}

	if err := f.blockBuilder.Reset(); err != nil {
		return nil, fmt.Errorf("failed to initialize block builder: %w", err)
	}

	if f.blockInfo.IsEndOfEpoch {
		extra.Validators = f.blockInfo.ValidatorSetDelta
	}

	txs, err := f.oracles.GetTransactions(f.blockInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get oracle transactions: %w", err)
	}

	for _, tx := range txs {
		if err := f.blockBuilder.WriteTx(tx); err != nil {
			return nil, fmt.Errorf("failed to apply oracle transaction: %w", err)
		}
	}

	// fill the block with transactions
	f.blockBuilder.Fill()

	extra.BlockMetaData = &polytypes.BlockMetaData{
		BlockRound:  currentRound,
		EpochNumber: f.blockInfo.CurrentEpoch,
	}

	stateBlock, err := f.blockBuilder.Build(func(h *types.Header) {
		h.ExtraData = extra.MarshalRLPTo(nil)
		h.MixHash = polytypes.PolyBFTMixDigest
	})

	if err != nil {
		return nil, err
	}

	if f.logger.GetLevel() <= hclog.Debug {
		blockMetaHash, err := extra.BlockMetaData.Hash(stateBlock.Block.Hash())
		if err != nil {
			return nil, fmt.Errorf("failed to calculate proposal hash: %w", err)
		}

		var buf bytes.Buffer

		for i, tx := range stateBlock.Block.Transactions {
			if f.logger.IsDebug() {
				buf.WriteString(tx.Hash().String())
			} else if f.logger.IsTrace() {
				buf.WriteString(tx.String())
			}

			if i != len(stateBlock.Block.Transactions)-1 {
				buf.WriteString("\n")
			}
		}

		f.logger.Debug("[FSM.BuildProposal]",
			"block num", stateBlock.Block.Number(),
			"round", currentRound,
			"state root", stateBlock.Block.Header.StateRoot,
			"proposal hash", blockMetaHash.String(),
			"txs count", len(stateBlock.Block.Transactions),
			"txs", buf.String(),
			"finsihedIn", time.Since(start),
		)
	}

	f.target = stateBlock

	return stateBlock.Block.MarshalRLP(), nil
}

// ValidateCommit is used to validate that a given commit is valid
func (f *fsm) ValidateCommit(signerAddr []byte, seal []byte, proposalHash []byte) error {
	from := types.BytesToAddress(signerAddr)

	validator := f.blockInfo.CurrentEpochValidatorSet.Accounts().GetValidatorMetadata(from)
	if validator == nil {
		return fmt.Errorf("unable to resolve validator %s", from)
	}

	signature, err := bls.UnmarshalSignature(seal)
	if err != nil {
		return fmt.Errorf("failed to unmarshall signature: %w", err)
	}

	if !signature.Verify(validator.BlsKey, proposalHash, signer.DomainBridge) {
		return fmt.Errorf("incorrect commit signature from %s", from)
	}

	return nil
}

// Validate validates a raw proposal (used if non-proposer)
func (f *fsm) Validate(proposal []byte) error {
	start := time.Now().UTC()
	parent := f.blockInfo.ParentBlock

	var block types.Block
	if err := block.UnmarshalRLP(proposal); err != nil {
		return fmt.Errorf("failed to validate, cannot decode block data. Error: %w", err)
	}

	// validate header fields
	if err := validateHeaderFields(parent, block.Header, f.config.BlockTimeDrift); err != nil {
		return fmt.Errorf(
			"failed to validate header (parent header# %d, current header#%d): %w",
			parent.Number,
			block.Number(),
			err,
		)
	}

	extra, err := polytypes.GetIbftExtra(block.Header.ExtraData)
	if err != nil {
		return fmt.Errorf("cannot get extra data:%w", err)
	}

	parentExtra, err := polytypes.GetIbftExtra(parent.ExtraData)
	if err != nil {
		return err
	}

	if extra.BlockMetaData == nil {
		return fmt.Errorf("block meta data for block %d is missing", block.Number())
	}

	if parentExtra.BlockMetaData == nil {
		return fmt.Errorf("block meta data for parent block %d is missing", parent.Number)
	}

	if err := extra.ValidateParentSignatures(block.Number(), f.polybftBackend, nil, parent, parentExtra,
		signer.DomainBridge, f.logger); err != nil {
		return err
	}

	if err := f.VerifyStateTransactions(block.Transactions); err != nil {
		return err
	}

	// validate validators delta
	if f.blockInfo.IsEndOfEpoch {
		if extra.Validators == nil {
			return errValidatorDeltaNilInEpochEndingBlock
		}

		if !extra.Validators.Equals(f.blockInfo.ValidatorSetDelta) {
			return errValidatorSetDeltaMismatch
		}
	} else if extra.Validators != nil {
		// delta should be nil in non epoch ending blocks
		return errValidatorsUpdateInNonEpochEnding
	}
	// validate block meta data
	if err := extra.BlockMetaData.Validate(parentExtra.BlockMetaData); err != nil {
		return err
	}

	if f.logger.IsTrace() && block.Number() > 1 {
		validators, err := f.polybftBackend.GetValidators(block.Number()-2, nil)
		if err != nil {
			return fmt.Errorf("failed to retrieve validators:%w", err)
		}

		f.logger.Trace("[FSM.Validate]", "block num", block.Number(), "parent validators", validators)
	}

	stateBlock, err := f.blockchain.ProcessBlock(f.blockInfo.ParentBlock, &block)
	if err != nil {
		return err
	}

	if f.logger.IsDebug() {
		blockMetaHash, err := extra.BlockMetaData.Hash(block.Hash())
		if err != nil {
			return fmt.Errorf("failed to calculate proposal hash: %w", err)
		}

		f.logger.Debug("[FSM.Validate]",
			"block num", block.Number(),
			"state root", block.Header.StateRoot,
			"proposer", types.BytesToHash(block.Header.Miner),
			"proposal hash", blockMetaHash,
			"finishedIn", time.Since(start),
		)
	}

	f.target = stateBlock

	return nil
}

// ValidateSender validates sender address and signature
func (f *fsm) ValidateSender(msg *proto.IbftMessage) error {
	msgNoSig, err := msg.PayloadNoSig()
	if err != nil {
		return err
	}

	signerAddress, err := wallet.RecoverAddressFromSignature(msg.Signature, msgNoSig)
	if err != nil {
		return fmt.Errorf("failed to recover address from signature: %w", err)
	}

	// verify the signature came from the sender
	if !bytes.Equal(msg.From, signerAddress.Bytes()) {
		return fmt.Errorf("signer address %s doesn't match From field", signerAddress.String())
	}

	// verify the sender is in the active validator set
	if !f.blockInfo.CurrentEpochValidatorSet.Includes(signerAddress) {
		return fmt.Errorf("signer address %s is not included in validator set", signerAddress.String())
	}

	return nil
}

// VerifyStateTransactions verifies the system transactions
func (f *fsm) VerifyStateTransactions(transactions []*types.Transaction) error {
	return f.oracles.VerifyTransactions(f.blockInfo, transactions)
}

// Insert inserts the sealed proposal
func (f *fsm) Insert(proposal []byte, committedSeals []*messages.CommittedSeal) (*types.FullBlock, error) {
	newBlock := f.target

	var proposedBlock types.Block
	if err := proposedBlock.UnmarshalRLP(proposal); err != nil {
		return nil, fmt.Errorf("failed to insert proposal, block unmarshaling failed: %w", err)
	}

	if newBlock == nil || newBlock.Block.Hash() != proposedBlock.Hash() {
		// if this is the case, we will let syncer insert the block
		return nil, errProposalDontMatch
	}

	// In this function we should try to return little to no errors since
	// at this point everything we have to do is just commit something that
	// we should have already computed beforehand.
	extra, err := polytypes.GetIbftExtra(newBlock.Block.Header.ExtraData)
	if err != nil {
		return nil, fmt.Errorf("failed to insert proposal, due to not being able to extract extra data: %w", err)
	}

	// create map for faster access to indexes
	nodeIDIndexMap := make(map[types.Address]int, f.blockInfo.CurrentEpochValidatorSet.Len())
	for i, addr := range f.blockInfo.CurrentEpochValidatorSet.Accounts().GetAddresses() {
		nodeIDIndexMap[addr] = i
	}

	// populated bitmap according to nodeId from validator set and committed seals
	// also populate slice of signatures
	bitmap := bitmap.Bitmap{}
	signatures := make(bls.Signatures, 0, len(committedSeals))

	for _, commSeal := range committedSeals {
		signerAddr := types.BytesToAddress(commSeal.Signer)

		index, exists := nodeIDIndexMap[signerAddr]
		if !exists {
			return nil, fmt.Errorf("invalid node id = %s", signerAddr.String())
		}

		s, err := bls.UnmarshalSignature(commSeal.Signature)
		if err != nil {
			return nil, fmt.Errorf("invalid signature = %s", commSeal.Signature)
		}

		signatures = append(signatures, s)

		bitmap.Set(uint64(index))
	}

	aggregatedSignature, err := signatures.Aggregate().Marshal()
	if err != nil {
		return nil, fmt.Errorf("could not aggregate seals: %w", err)
	}

	// include aggregated signature of all committed seals
	// also includes bitmap which contains all indexes from validator set which provides there seals
	extra.Committed = &polytypes.Signature{
		AggregatedSignature: aggregatedSignature,
		Bitmap:              bitmap,
	}

	// Write extra data to header
	newBlock.Block.Header.ExtraData = extra.MarshalRLPTo(nil)

	if err := f.blockchain.CommitBlock(newBlock); err != nil {
		return nil, err
	}

	return newBlock, nil
}

// Height returns the height for the current round
func (f *fsm) Height() uint64 {
	return f.blockInfo.CurrentBlock()
}

func validateHeaderFields(parent *types.Header, header *types.Header, blockTimeDrift uint64) error {
	// header extra data must be higher or equal to ExtraVanity = 32 in order to be compliant with Ethereum blocks
	if len(header.ExtraData) < polytypes.ExtraVanity {
		return fmt.Errorf("extra-data shorter than %d bytes (%d)", polytypes.ExtraVanity, len(header.ExtraData))
	}
	// verify parent hash
	if parent.Hash != header.ParentHash {
		return fmt.Errorf("incorrect header parent hash (parent=%s, header parent=%s)", parent.Hash, header.ParentHash)
	}
	// verify parent number
	if header.Number != parent.Number+1 {
		return fmt.Errorf("invalid number")
	}
	// verify time is from the future
	if header.Timestamp > (uint64(time.Now().UTC().Unix()) + blockTimeDrift) {
		return fmt.Errorf("block from the future. block timestamp: %s, configured block time drift %d seconds",
			time.Unix(int64(header.Timestamp), 0).Format(time.RFC3339), blockTimeDrift)
	}
	// verify header nonce is zero
	if header.Nonce != types.ZeroNonce {
		return fmt.Errorf("invalid nonce")
	}
	// verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gas limit: have %v, max %v", header.GasUsed, header.GasLimit)
	}
	// verify time has passed
	if header.Timestamp <= parent.Timestamp {
		return fmt.Errorf("timestamp older than parent")
	}
	// verify mix digest
	if header.MixHash != polytypes.PolyBFTMixDigest {
		return fmt.Errorf("mix digest is not correct")
	}
	// difficulty must be > 0
	if header.Difficulty <= 0 {
		return fmt.Errorf("difficulty should be greater than zero")
	}
	// calculated header hash must be correct
	if header.Hash != types.HeaderHash(header) {
		return fmt.Errorf("invalid header hash")
	}

	return nil
}
