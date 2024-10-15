package polybft

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/0xPolygon/go-ibft/messages"
	"github.com/0xPolygon/go-ibft/messages/proto"
	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/oracle"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	polytesting "github.com/0xPolygon/polygon-edge/consensus/polybft/testing"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestFSM_ValidateHeader(t *testing.T) {
	t.Parallel()

	blockTimeDrift := uint64(1)
	extra := createTestExtra(validator.AccountSet{}, validator.AccountSet{}, 0, 0, 0)
	parent := &types.Header{Number: 0, Hash: types.BytesToHash([]byte{1, 2, 3})}
	header := &types.Header{Number: 0}

	// extra data
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "extra-data shorter than")
	header.ExtraData = extra

	// parent hash
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "incorrect header parent hash")
	header.ParentHash = parent.Hash

	// sequence number
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "invalid number")
	header.Number = 1

	// failed timestamp
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "timestamp older than parent")
	header.Timestamp = 10

	// failed nonce
	header.SetNonce(1)
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "invalid nonce")

	header.SetNonce(0)

	// failed gas
	header.GasLimit = 10
	header.GasUsed = 11
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "invalid gas limit")
	header.GasLimit = 10
	header.GasUsed = 10

	// mix digest
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "mix digest is not correct")
	header.MixHash = polytypes.PolyBFTMixDigest

	// difficulty
	header.Difficulty = 0
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "difficulty should be greater than zero")

	header.Difficulty = 1
	header.Hash = types.BytesToHash([]byte{11, 22, 33})
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "invalid header hash")
	header.Timestamp = uint64(time.Now().UTC().Unix() + 150)
	require.ErrorContains(t, validateHeaderFields(parent, header, blockTimeDrift), "block from the future")

	header.Timestamp = uint64(time.Now().UTC().Unix())

	header.ComputeHash()
	require.NoError(t, validateHeaderFields(parent, header, blockTimeDrift))
}

func TestFSM_BuildProposal_WithoutCommitEpochTxGood(t *testing.T) {
	t.Parallel()

	const (
		accountCount             = 5
		committedCount           = 4
		parentCount              = 3
		confirmedStateSyncsCount = 5
		parentBlockNumber        = 1023
		currentRound             = 1
	)

	validators := validator.NewTestValidators(t, accountCount)
	validatorSet := validators.GetPublicIdentities()
	extra := createTestExtra(validatorSet, validator.AccountSet{}, accountCount-1, committedCount, parentCount)

	parent := &types.Header{Number: parentBlockNumber, ExtraData: extra}
	parent.ComputeHash()
	stateBlock := createDummyStateBlock(parentBlockNumber+1, parent.Hash, extra)
	mBlockBuilder := newBlockBuilderMock(stateBlock)

	runtime := &consensusRuntime{
		logger: hclog.NewNullLogger(),
		config: &config.Runtime{
			Key: wallet.NewKey(validators.GetPrivateIdentities()[0]),
		},
	}

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			CurrentEpoch:             1,
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validators.ToValidatorSet(),
		},
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		logger:       hclog.NewNullLogger(),
	}

	proposal, err := fsm.BuildProposal(currentRound)
	assert.NoError(t, err)
	assert.NotNil(t, proposal)

	rlpBlock := stateBlock.Block.MarshalRLP()
	assert.Equal(t, rlpBlock, proposal)

	block := types.Block{}
	require.NoError(t, block.UnmarshalRLP(proposal))

	blockMeta := &polytypes.BlockMetaData{
		BlockRound:  currentRound,
		EpochNumber: fsm.blockInfo.CurrentEpoch,
	}

	blockMetaHash, err := blockMeta.Hash(block.Hash())
	require.NoError(t, err)

	msg := runtime.BuildPrePrepareMessage(proposal, nil, &proto.View{})
	require.Equal(t, blockMetaHash.Bytes(), msg.GetPreprepareData().ProposalHash)

	mBlockBuilder.AssertExpectations(t)
}

func TestFSM_BuildProposal_EpochEndingBlock_ValidatorsDeltaExists(t *testing.T) {
	t.Parallel()

	const (
		validatorsCount          = 6
		remainingValidatorsCount = 3
		signaturesCount          = 4
		parentBlockNumber        = 49
	)

	validators := validator.NewTestValidators(t, validatorsCount).GetPublicIdentities()
	extra := createTestExtraObject(validators, validator.AccountSet{}, validatorsCount-1, signaturesCount, signaturesCount)
	extra.Validators = nil

	extraData := extra.MarshalRLPTo(nil)
	parent := &types.Header{Number: parentBlockNumber, ExtraData: extraData}
	parent.ComputeHash()
	stateBlock := createDummyStateBlock(parentBlockNumber+1, parent.Hash, extraData)

	blockBuilderMock := newBlockBuilderMock(stateBlock)

	addedValidators := validator.NewTestValidators(t, 2).GetPublicIdentities()
	removedValidators := [3]uint64{3, 4, 5}
	removedBitmap := &bitmap.Bitmap{}

	for _, i := range removedValidators {
		removedBitmap.Set(i)
	}

	newDelta := &validator.ValidatorSetDelta{
		Added:   addedValidators,
		Updated: validator.AccountSet{},
		Removed: *removedBitmap,
	}

	blockChainMock := new(polychain.BlockchainMock)

	validatorSet := validator.NewValidatorSet(validators, hclog.NewNullLogger())

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			ValidatorSetDelta:        newDelta,
			CurrentEpochValidatorSet: validatorSet,
			IsEndOfEpoch:             true,
		},
		blockBuilder: blockBuilderMock,
		config:       &config.PolyBFT{},
		blockchain:   blockChainMock,
		logger:       hclog.NewNullLogger(),
	}

	proposal, err := fsm.BuildProposal(0)
	assert.NoError(t, err)
	assert.NotNil(t, proposal)

	blockExtra, err := polytypes.GetIbftExtra(stateBlock.Block.Header.ExtraData)
	assert.NoError(t, err)
	assert.Len(t, blockExtra.Validators.Added, 2)
	assert.False(t, blockExtra.Validators.IsEmpty())

	for _, addedValidator := range addedValidators {
		assert.True(t, blockExtra.Validators.Added.ContainsAddress(addedValidator.Address))
	}

	for _, removedValidator := range removedValidators {
		assert.True(
			t,
			blockExtra.Validators.Removed.IsSet(removedValidator),
			fmt.Sprintf("Expected validator at index %d to be marked as removed, but it wasn't", removedValidator),
		)
	}

	blockBuilderMock.AssertExpectations(t)
	blockChainMock.AssertExpectations(t)
}

func TestFSM_BuildProposal_NonEpochEndingBlock_ValidatorsDeltaNil(t *testing.T) {
	t.Parallel()

	const (
		accountCount      = 6
		signaturesCount   = 4
		parentBlockNumber = 9
	)

	testValidators := validator.NewTestValidators(t, accountCount)
	extra := createTestExtra(testValidators.GetPublicIdentities(), validator.AccountSet{}, accountCount-1, signaturesCount, signaturesCount)
	parent := &types.Header{Number: parentBlockNumber, ExtraData: extra}
	parent.ComputeHash()
	stateBlock := createDummyStateBlock(parentBlockNumber+1, parent.Hash, extra)

	blockBuilderMock := new(polychain.BlockBuilderMock)
	blockBuilderMock.On("Build", mock.Anything).Return(stateBlock).Once()
	blockBuilderMock.On("Fill").Once()
	blockBuilderMock.On("Reset").Return(error(nil)).Once()

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			IsEndOfEpoch:             false,
			CurrentEpochValidatorSet: testValidators.ToValidatorSet(),
		},
		blockBuilder: blockBuilderMock,
		config:       &config.PolyBFT{},
		blockchain:   new(polychain.BlockchainMock),
		logger:       hclog.NewNullLogger(),
	}

	proposal, err := fsm.BuildProposal(0)
	assert.NoError(t, err)
	assert.NotNil(t, proposal)

	blockExtra, err := polytypes.GetIbftExtra(stateBlock.Block.Header.ExtraData)
	assert.NoError(t, err)
	assert.Nil(t, blockExtra.Validators)

	blockBuilderMock.AssertExpectations(t)
}

func TestFSM_ValidateCommit_WrongValidator(t *testing.T) {
	t.Parallel()

	const (
		accountsCount     = 5
		parentBlockNumber = 10
	)

	validators := validator.NewTestValidators(t, accountsCount)
	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, 5, 3, 3),
	}
	parent.ComputeHash()
	stateBlock := createDummyStateBlock(parentBlockNumber+1, parent.Hash, parent.ExtraData)
	mBlockBuilder := newBlockBuilderMock(stateBlock)

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validators.ToValidatorSet(),
		},
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		logger:       hclog.NewNullLogger(),
	}

	_, err := fsm.BuildProposal(0)
	require.NoError(t, err)

	err = fsm.ValidateCommit([]byte("0x7467674"), types.ZeroAddress.Bytes(), []byte{})
	require.ErrorContains(t, err, "unable to resolve validator")
}

func TestFSM_ValidateCommit_InvalidHash(t *testing.T) {
	t.Parallel()

	const (
		accountsCount     = 5
		parentBlockNumber = 10
	)

	validators := validator.NewTestValidators(t, accountsCount)

	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, 5, 3, 3),
	}
	parent.ComputeHash()
	stateBlock := createDummyStateBlock(parentBlockNumber+1, parent.Hash, parent.ExtraData)
	mBlockBuilder := newBlockBuilderMock(stateBlock)

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validators.ToValidatorSet(),
		},
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		logger:       hclog.NewNullLogger(),
	}

	_, err := fsm.BuildProposal(0)
	require.NoError(t, err)

	nonValidatorAcc := validator.NewTestValidator(t, "non_validator", 1)
	wrongSignature, err := nonValidatorAcc.MustSign([]byte("Foo"), signer.DomainBridge).Marshal()
	require.NoError(t, err)

	err = fsm.ValidateCommit(validators.GetValidator("0").Address().Bytes(), wrongSignature, []byte{})
	require.ErrorContains(t, err, "incorrect commit signature from")
}

func TestFSM_ValidateCommit_Good(t *testing.T) {
	t.Parallel()

	const parentBlockNumber = 10

	validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E"})
	validatorsMetadata := validators.GetPublicIdentities()

	parent := &types.Header{Number: parentBlockNumber, ExtraData: createTestExtra(validatorsMetadata, validator.AccountSet{}, 5, 3, 3)}
	parent.ComputeHash()
	stateBlock := createDummyStateBlock(parentBlockNumber+1, parent.Hash, parent.ExtraData)
	mBlockBuilder := newBlockBuilderMock(stateBlock)

	validatorSet := validator.NewValidatorSet(validatorsMetadata, hclog.NewNullLogger())

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validatorSet,
		},
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		logger:       hclog.NewNullLogger(),
	}

	proposal, err := fsm.BuildProposal(0)
	require.NoError(t, err)

	block := types.Block{}
	require.NoError(t, block.UnmarshalRLP(proposal))

	validator := validators.GetValidator("A")
	seal, err := validator.MustSign(block.Hash().Bytes(), signer.DomainBridge).Marshal()
	require.NoError(t, err)
	err = fsm.ValidateCommit(validator.Key().Address().Bytes(), seal, block.Hash().Bytes())
	require.NoError(t, err)
}

func TestFSM_Validate_EpochEndingBlock_MismatchInDeltas(t *testing.T) {
	const (
		accountsCount     = 5
		parentBlockNumber = 25
		signaturesCount   = 3
	)

	validators := validator.NewTestValidators(t, accountsCount)
	parentExtra := createTestExtraObject(validators.GetPublicIdentities(), validator.AccountSet{}, 4, signaturesCount, signaturesCount)
	parentExtra.Validators = nil

	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: parentExtra.MarshalRLPTo(nil),
	}
	parent.ComputeHash()

	polybftBackendMock := new(polytypes.PolybftBackendMock)
	polybftBackendMock.On("GetValidators", mock.Anything, mock.Anything).Return(validators.GetPublicIdentities(), nil).Once()

	extra := createTestExtraObject(validators.GetPublicIdentities(), validator.AccountSet{}, 4, signaturesCount, signaturesCount)
	parentBlockMetaHash, err := extra.BlockMetaData.Hash(parent.Hash)
	require.NoError(t, err)

	extra.Validators = &validator.ValidatorSetDelta{} // this will cause test to fail
	extra.Parent = createSignature(t, validators.GetPrivateIdentities(), parentBlockMetaHash, signer.DomainBridge)

	stateBlock := createDummyStateBlock(parent.Number+1, types.Hash{100, 15}, extra.MarshalRLPTo(nil))

	proposalHash, err := new(polytypes.BlockMetaData).Hash(stateBlock.Block.Hash())
	require.NoError(t, err)

	// a new validator is added to delta which proposers block does not have
	privateKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	newValidatorDelta := &validator.ValidatorSetDelta{
		Added: validator.AccountSet{&validator.ValidatorMetadata{
			Address:     types.BytesToAddress([]byte{0, 1, 2, 3}),
			BlsKey:      privateKey.PublicKey(),
			VotingPower: new(big.Int).SetUint64(1),
			IsActive:    true,
		}},
	}

	stateBlock.Block.Header.Hash = proposalHash
	stateBlock.Block.Header.ParentHash = parent.Hash
	stateBlock.Block.Header.Timestamp = uint64(time.Now().UTC().Unix())

	proposal := stateBlock.Block.MarshalRLP()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("ProcessBlock", mock.Anything, mock.Anything).
		Return(stateBlock, error(nil)).
		Maybe()

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validators.ToValidatorSet(),
			ValidatorSetDelta:        newValidatorDelta,
			IsEndOfEpoch:             true,
		},
		blockchain:     blockchainMock,
		logger:         hclog.NewNullLogger(),
		polybftBackend: polybftBackendMock,
		config:         &config.PolyBFT{BlockTimeDrift: 1},
	}

	err = fsm.Validate(proposal)
	require.ErrorIs(t, err, errValidatorSetDeltaMismatch)

	polybftBackendMock.AssertExpectations(t)
	blockchainMock.AssertExpectations(t)
}

func TestFSM_Validate_EpochEndingBlock_UpdatingValidatorSetInNonEpochEndingBlock(t *testing.T) {
	t.Parallel()

	const (
		accountsCount     = 5
		parentBlockNumber = 25
		signaturesCount   = 3
	)

	validators := validator.NewTestValidators(t, accountsCount)
	parentExtra := createTestExtraObject(validators.GetPublicIdentities(), validator.AccountSet{}, 4, signaturesCount, signaturesCount)
	parentExtra.Validators = nil

	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: parentExtra.MarshalRLPTo(nil),
	}
	parent.ComputeHash()

	polybftBackendMock := new(polytypes.PolybftBackendMock)
	polybftBackendMock.On("GetValidators", mock.Anything, mock.Anything).Return(validators.GetPublicIdentities(), nil).Once()

	// a new validator is added to delta which proposers block does not have
	privateKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	newValidatorDelta := &validator.ValidatorSetDelta{
		Added: validator.AccountSet{&validator.ValidatorMetadata{
			Address:     types.BytesToAddress([]byte{0, 1, 2, 3}),
			BlsKey:      privateKey.PublicKey(),
			VotingPower: new(big.Int).SetUint64(1),
			IsActive:    true,
		}},
	}

	extra := createTestExtraObject(validators.GetPublicIdentities(), validator.AccountSet{}, 4, signaturesCount, signaturesCount)
	parentBlockMetaHash, err := extra.BlockMetaData.Hash(parent.Hash)
	require.NoError(t, err)

	extra.Validators = newValidatorDelta // this will cause test to fail
	extra.Parent = createSignature(t, validators.GetPrivateIdentities(), parentBlockMetaHash, signer.DomainBridge)

	stateBlock := createDummyStateBlock(parent.Number+1, types.Hash{100, 15}, extra.MarshalRLPTo(nil))

	proposalHash, err := new(polytypes.BlockMetaData).Hash(stateBlock.Block.Hash())
	require.NoError(t, err)

	stateBlock.Block.Header.Hash = proposalHash
	stateBlock.Block.Header.ParentHash = parent.Hash
	stateBlock.Block.Header.Timestamp = uint64(time.Now().UTC().Unix())

	proposal := stateBlock.Block.MarshalRLP()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("ProcessBlock", mock.Anything, mock.Anything).
		Return(stateBlock, error(nil)).
		Maybe()

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validators.ToValidatorSet(),
		},
		blockchain:     blockchainMock,
		logger:         hclog.NewNullLogger(),
		polybftBackend: polybftBackendMock,
		config:         &config.PolyBFT{BlockTimeDrift: 1},
	}

	err = fsm.Validate(proposal)
	require.ErrorIs(t, err, errValidatorsUpdateInNonEpochEnding)

	polybftBackendMock.AssertExpectations(t)
	blockchainMock.AssertExpectations(t)
}

func TestFSM_Validate_IncorrectHeaderParentHash(t *testing.T) {
	t.Parallel()

	const (
		accountsCount     = 5
		parentBlockNumber = 25
		signaturesCount   = 3
	)

	validators := validator.NewTestValidators(t, accountsCount)
	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, 4, signaturesCount, signaturesCount),
	}
	parent.ComputeHash()

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validators.ToValidatorSet(),
		},
		blockchain: &polychain.BlockchainMock{},
		logger:     hclog.NewNullLogger(),
		config: &config.PolyBFT{
			BlockTimeDrift: 1,
		},
	}

	stateBlock := createDummyStateBlock(parent.Number+1, types.Hash{100, 15}, parent.ExtraData)

	hash, err := new(polytypes.BlockMetaData).Hash(stateBlock.Block.Hash())
	require.NoError(t, err)

	stateBlock.Block.Header.Hash = hash
	proposal := stateBlock.Block.MarshalRLP()

	err = fsm.Validate(proposal)
	require.ErrorContains(t, err, "incorrect header parent hash")
}

func TestFSM_Validate_InvalidNumber(t *testing.T) {
	t.Parallel()

	const (
		accountsCount     = 5
		parentBlockNumber = 10
		signaturesCount   = 3
	)

	validators := validator.NewTestValidators(t, accountsCount)
	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, 4, signaturesCount, signaturesCount),
	}
	parent.ComputeHash()

	// try some invalid block numbers, parentBlockNumber + 1 should be correct
	for _, blockNum := range []uint64{parentBlockNumber - 1, parentBlockNumber, parentBlockNumber + 2} {
		stateBlock := createDummyStateBlock(blockNum, parent.Hash, parent.ExtraData)
		mBlockBuilder := newBlockBuilderMock(stateBlock)
		fsm := &fsm{
			blockInfo: oracle.NewBlockInfo{
				ParentBlock:              parent,
				CurrentEpochValidatorSet: validators.ToValidatorSet(),
			},
			blockBuilder: mBlockBuilder,
			blockchain:   &polychain.BlockchainMock{},
			logger:       hclog.NewNullLogger(),
			config:       &config.PolyBFT{BlockTimeDrift: 1},
		}

		proposalHash, err := new(polytypes.BlockMetaData).Hash(stateBlock.Block.Hash())
		require.NoError(t, err)

		stateBlock.Block.Header.Hash = proposalHash
		proposal := stateBlock.Block.MarshalRLP()

		err = fsm.Validate(proposal)
		require.ErrorContains(t, err, "invalid number")
	}
}

func TestFSM_Validate_TimestampOlder(t *testing.T) {
	t.Parallel()

	const parentBlockNumber = 10

	validators := validator.NewTestValidators(t, 5)
	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, 4, 3, 3),
		Timestamp: uint64(time.Now().UTC().Unix()),
	}
	parent.ComputeHash()

	// try some invalid times
	for _, blockTime := range []uint64{parent.Timestamp - 1, parent.Timestamp} {
		header := &types.Header{
			Number:     parentBlockNumber + 1,
			ParentHash: parent.Hash,
			Timestamp:  blockTime,
			ExtraData:  parent.ExtraData,
		}
		stateBlock := &types.FullBlock{Block: consensus.BuildBlock(consensus.BuildBlockParams{Header: header})}
		fsm := &fsm{
			blockInfo: oracle.NewBlockInfo{
				ParentBlock:              parent,
				CurrentEpochValidatorSet: validators.ToValidatorSet(),
			},
			blockchain: &polychain.BlockchainMock{},
			logger:     hclog.NewNullLogger(),
			config: &config.PolyBFT{
				BlockTimeDrift: 1,
			}}

		blocMetaHash, err := new(polytypes.BlockMetaData).Hash(header.Hash)
		require.NoError(t, err)

		stateBlock.Block.Header.Hash = blocMetaHash
		proposal := stateBlock.Block.MarshalRLP()

		err = fsm.Validate(proposal)
		assert.ErrorContains(t, err, "timestamp older than parent")
	}
}

func TestFSM_Validate_IncorrectMixHash(t *testing.T) {
	t.Parallel()

	const parentBlockNumber = 10

	validators := validator.NewTestValidators(t, 5)
	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, 4, 3, 3),
		Timestamp: uint64(100),
	}
	parent.ComputeHash()

	header := &types.Header{
		Number:     parentBlockNumber + 1,
		ParentHash: parent.Hash,
		Timestamp:  parent.Timestamp + 1,
		MixHash:    types.Hash{},
		ExtraData:  parent.ExtraData,
	}

	buildBlock := &types.FullBlock{Block: consensus.BuildBlock(consensus.BuildBlockParams{Header: header})}

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validators.ToValidatorSet(),
		}, blockchain: &polychain.BlockchainMock{},
		logger: hclog.NewNullLogger(),
		config: &config.PolyBFT{
			BlockTimeDrift: 1,
		},
	}
	rlpBlock := buildBlock.Block.MarshalRLP()

	_, err := new(polytypes.BlockMetaData).Hash(header.Hash)
	require.NoError(t, err)

	err = fsm.Validate(rlpBlock)
	assert.ErrorContains(t, err, "mix digest is not correct")
}

func TestFSM_Insert_Good(t *testing.T) {
	t.Parallel()

	const (
		accountCount      = 5
		parentBlockNumber = uint64(10)
		signaturesCount   = 3
	)

	setupFn := func() (*fsm, []*messages.CommittedSeal, *types.FullBlock, *polychain.BlockchainMock) {
		validators := validator.NewTestValidators(t, accountCount)
		allAccounts := validators.GetPrivateIdentities()
		validatorsMetadata := validators.GetPublicIdentities()

		extraParent := createTestExtra(validatorsMetadata, validator.AccountSet{}, len(allAccounts)-1, signaturesCount, signaturesCount)
		parent := &types.Header{Number: parentBlockNumber, ExtraData: extraParent}
		extraBlock := createTestExtra(validatorsMetadata, validator.AccountSet{}, len(allAccounts)-1, signaturesCount, signaturesCount)
		block := consensus.BuildBlock(
			consensus.BuildBlockParams{
				Header: &types.Header{Number: parentBlockNumber + 1, ParentHash: parent.Hash, ExtraData: extraBlock},
			})

		builtBlock := &types.FullBlock{Block: block}

		builderMock := newBlockBuilderMock(builtBlock)
		chainMock := new(polychain.BlockchainMock)
		chainMock.On("CommitBlock", mock.Anything).Return(error(nil)).Once()
		chainMock.On("ProcessBlock", mock.Anything, mock.Anything).
			Return(builtBlock, error(nil)).
			Maybe()

		f := &fsm{
			blockInfo: oracle.NewBlockInfo{
				ParentBlock: parent,
				CurrentEpochValidatorSet: validator.NewValidatorSet(validatorsMetadata[0:len(validatorsMetadata)-1],
					hclog.NewNullLogger()),
			},
			blockBuilder: builderMock,
			target:       builtBlock,
			blockchain:   chainMock,
			logger:       hclog.NewNullLogger(),
		}

		seals := make([]*messages.CommittedSeal, signaturesCount)

		for i := 0; i < signaturesCount; i++ {
			sign, err := allAccounts[i].Bls.Sign(builtBlock.Block.Hash().Bytes(), signer.DomainBridge)
			require.NoError(t, err)
			sigRaw, err := sign.Marshal()
			require.NoError(t, err)

			seals[i] = &messages.CommittedSeal{
				Signer:    validatorsMetadata[i].Address.Bytes(),
				Signature: sigRaw,
			}
		}

		return f, seals, builtBlock, chainMock
	}

	t.Run("Insert with target block defined", func(t *testing.T) {
		t.Parallel()

		fsm, seals, builtBlock, chainMock := setupFn()
		proposal := builtBlock.Block.MarshalRLP()
		fullBlock, err := fsm.Insert(proposal, seals)

		require.NoError(t, err)
		require.Equal(t, parentBlockNumber+1, fullBlock.Block.Number())
		chainMock.AssertExpectations(t)
	})

	t.Run("Insert with target block undefined", func(t *testing.T) {
		t.Parallel()

		fsm, seals, builtBlock, _ := setupFn()
		fsm.target = nil
		proposal := builtBlock.Block.MarshalRLP()
		_, err := fsm.Insert(proposal, seals)

		require.ErrorIs(t, err, errProposalDontMatch)
	})

	t.Run("Insert with target block hash not match", func(t *testing.T) {
		t.Parallel()

		fsm, seals, builtBlock, _ := setupFn()
		proposal := builtBlock.Block.MarshalRLP()
		fsm.target = builtBlock
		fsm.target.Block.Header.Hash = types.BytesToHash(polytesting.GenerateRandomBytes(t))
		_, err := fsm.Insert(proposal, seals)

		require.ErrorIs(t, err, errProposalDontMatch)
	})
}

func TestFSM_Insert_InvalidNode(t *testing.T) {
	t.Parallel()

	const (
		accountCount      = 5
		parentBlockNumber = 10
		signaturesCount   = 3
	)

	validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E"})
	validatorsMetadata := validators.GetPublicIdentities()

	parent := &types.Header{Number: parentBlockNumber}
	parent.ComputeHash()

	extraBlock := createTestExtra(validatorsMetadata, validator.AccountSet{}, len(validators.Validators)-1, signaturesCount, signaturesCount)
	finalBlock := consensus.BuildBlock(
		consensus.BuildBlockParams{
			Header: &types.Header{Number: parentBlockNumber + 1, ParentHash: parent.Hash, ExtraData: extraBlock},
		})

	buildBlock := &types.FullBlock{Block: finalBlock, Receipts: []*types.Receipt{}}
	mBlockBuilder := newBlockBuilderMock(buildBlock)

	validatorSet := validator.NewValidatorSet(validatorsMetadata[0:len(validatorsMetadata)-1], hclog.NewNullLogger())

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validatorSet,
		},
		blockBuilder: mBlockBuilder,
		blockchain:   &polychain.BlockchainMock{},
	}

	proposal := buildBlock.Block.MarshalRLP()
	validatorA := validators.GetValidator("A")
	validatorB := validators.GetValidator("B")
	proposalHash := buildBlock.Block.Hash().Bytes()
	sigA, err := validatorA.MustSign(proposalHash, signer.DomainBridge).Marshal()
	require.NoError(t, err)

	sigB, err := validatorB.MustSign(proposalHash, signer.DomainBridge).Marshal()
	require.NoError(t, err)

	// create test account outside of validator set
	nonValidatorAccount := validator.NewTestValidator(t, "non_validator", 1)
	nonValidatorSignature, err := nonValidatorAccount.MustSign(proposalHash, signer.DomainBridge).Marshal()
	require.NoError(t, err)

	commitedSeals := []*messages.CommittedSeal{
		{Signer: validatorA.Address().Bytes(), Signature: sigA},
		{Signer: validatorB.Address().Bytes(), Signature: sigB},
		{Signer: nonValidatorAccount.Address().Bytes(), Signature: nonValidatorSignature}, // this one should fail
	}

	fsm.target = buildBlock

	_, err = fsm.Insert(proposal, commitedSeals)
	assert.ErrorContains(t, err, "invalid node id")
}

func TestFSM_Height(t *testing.T) {
	t.Parallel()

	parentNumber := uint64(3)
	parent := &types.Header{Number: parentNumber}
	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{ParentBlock: parent},
	}
	assert.Equal(t, parentNumber+1, fsm.Height())
}

func TestFSM_Validate_FailToVerifySignatures(t *testing.T) {
	t.Parallel()

	const (
		accountsCount     = 5
		parentBlockNumber = 10
		signaturesCount   = 3
	)

	validators := validator.NewTestValidators(t, accountsCount)
	validatorsMetadata := validators.GetPublicIdentities()

	extra := createTestExtraObject(validatorsMetadata, validator.AccountSet{}, 4, signaturesCount, signaturesCount)

	extra.BlockMetaData = &polytypes.BlockMetaData{}
	parent := &types.Header{
		Number:    parentBlockNumber,
		ExtraData: extra.MarshalRLPTo(nil),
	}
	parent.ComputeHash()

	polybftBackendMock := new(polytypes.PolybftBackendMock)
	polybftBackendMock.On("GetValidators", mock.Anything, mock.Anything).Return(validatorsMetadata, nil).Once()

	validatorSet := validator.NewValidatorSet(validatorsMetadata, hclog.NewNullLogger())

	fsm := &fsm{
		blockInfo: oracle.NewBlockInfo{
			ParentBlock:              parent,
			CurrentEpochValidatorSet: validatorSet,
		},
		blockchain:     &polychain.BlockchainMock{},
		polybftBackend: polybftBackendMock,
		logger:         hclog.NewNullLogger(),
		config: &config.PolyBFT{
			BlockTimeDrift: 1,
		},
	}

	finalBlock := consensus.BuildBlock(consensus.BuildBlockParams{
		Header: &types.Header{
			Number:     parentBlockNumber + 1,
			ParentHash: parent.Hash,
			Timestamp:  parent.Timestamp + 1,
			MixHash:    polytypes.PolyBFTMixDigest,
			Difficulty: 1,
			ExtraData:  parent.ExtraData,
		},
	})

	blockMetaHash, err := new(polytypes.BlockMetaData).Hash(finalBlock.Hash())
	require.NoError(t, err)

	finalBlock.Header.Hash = blockMetaHash
	proposal := finalBlock.MarshalRLP()

	assert.ErrorContains(t, fsm.Validate(proposal), "failed to verify signatures")

	polybftBackendMock.AssertExpectations(t)
}

func createDummyStateBlock(blockNumber uint64, parentHash types.Hash, extraData []byte) *types.FullBlock {
	finalBlock := consensus.BuildBlock(consensus.BuildBlockParams{
		Header: &types.Header{
			Number:     blockNumber,
			ParentHash: parentHash,
			Difficulty: 1,
			ExtraData:  extraData,
			MixHash:    polytypes.PolyBFTMixDigest,
		},
	})

	return &types.FullBlock{Block: finalBlock}
}

func createTestExtra(
	allAccounts,
	previousValidatorSet validator.AccountSet,
	validatorsCount,
	committedSignaturesCount,
	parentSignaturesCount int,
) []byte {
	extraData := createTestExtraObject(allAccounts, previousValidatorSet, validatorsCount, committedSignaturesCount, parentSignaturesCount)

	return extraData.MarshalRLPTo(nil)
}

func newBlockBuilderMock(stateBlock *types.FullBlock) *polychain.BlockBuilderMock {
	mBlockBuilder := new(polychain.BlockBuilderMock)
	mBlockBuilder.On("Build", mock.Anything).Return(stateBlock).Once()
	mBlockBuilder.On("Fill", mock.Anything).Once()
	mBlockBuilder.On("Reset", mock.Anything).Return(error(nil)).Once()

	return mBlockBuilder
}

func createTestExtraObject(allAccounts,
	previousValidatorSet validator.AccountSet,
	validatorsCount,
	committedSignaturesCount,
	parentSignaturesCount int) *polytypes.Extra {
	accountCount := len(allAccounts)
	dummySignature := [64]byte{}
	bitmapCommitted, bitmapParent := bitmap.Bitmap{}, bitmap.Bitmap{}
	extraData := &polytypes.Extra{}
	extraData.Validators = generateValidatorDelta(validatorsCount, allAccounts, previousValidatorSet)

	for j := range rand.Perm(accountCount)[:committedSignaturesCount] {
		bitmapCommitted.Set(uint64(j))
	}

	for j := range rand.Perm(accountCount)[:parentSignaturesCount] {
		bitmapParent.Set(uint64(j))
	}

	extraData.Parent = &polytypes.Signature{Bitmap: bitmapCommitted, AggregatedSignature: dummySignature[:]}
	extraData.Committed = &polytypes.Signature{Bitmap: bitmapParent, AggregatedSignature: dummySignature[:]}
	extraData.BlockMetaData = &polytypes.BlockMetaData{}

	return extraData
}

func generateValidatorDelta(validatorCount int, allAccounts, previousValidatorSet validator.AccountSet) (vd *validator.ValidatorSetDelta) {
	oldMap := make(map[types.Address]int, previousValidatorSet.Len())
	for i, x := range previousValidatorSet {
		oldMap[x.Address] = i
	}

	vd = &validator.ValidatorSetDelta{}
	vd.Removed = bitmap.Bitmap{}

	for _, id := range rand.Perm(len(allAccounts))[:validatorCount] {
		_, exists := oldMap[allAccounts[id].Address]
		if !exists {
			vd.Added = append(vd.Added, allAccounts[id])
		}

		delete(oldMap, allAccounts[id].Address)
	}

	for _, v := range oldMap {
		vd.Removed.Set(uint64(v))
	}

	return
}
