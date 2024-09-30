package polybft

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/0xPolygon/go-ibft/messages"
	"github.com/0xPolygon/go-ibft/messages/proto"
	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bridge"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	polytesting "github.com/0xPolygon/polygon-edge/consensus/polybft/testing"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/contracts"
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

func TestFSM_verifyCommitEpochTx(t *testing.T) {
	t.Parallel()

	fsm := &fsm{
		isEndOfEpoch:     true,
		commitEpochInput: createTestCommitEpochInput(t, 0, 10),
		parent:           &types.Header{},
	}

	// include commit epoch transaction to the epoch ending block
	commitEpochTx, err := fsm.createCommitEpochTx()
	assert.NoError(t, err)
	assert.NotNil(t, commitEpochTx)

	assert.NoError(t, fsm.verifyCommitEpochTx(commitEpochTx))

	// submit tampered commit epoch transaction to the epoch ending block
	alteredCommitEpochTx := types.NewTx(types.NewStateTx(
		types.WithTo(&contracts.EpochManagerContract),
		types.WithInput([]byte{}),
		types.WithGas(0),
	))

	assert.ErrorContains(t, fsm.verifyCommitEpochTx(alteredCommitEpochTx), "invalid commit epoch transaction")

	// submit validators commit epoch transaction to the non-epoch ending block
	fsm.isEndOfEpoch = false
	commitEpochTx, err = fsm.createCommitEpochTx()
	assert.NoError(t, err)
	assert.NotNil(t, commitEpochTx)
	assert.ErrorContains(t, fsm.verifyCommitEpochTx(commitEpochTx), errCommitEpochTxNotExpected.Error())
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
		parent:       parent,
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		validators:   validators.ToValidatorSet(),
		logger:       hclog.NewNullLogger(),
		forks:        &chain.Forks{chain.Governance: chain.NewFork(0)},
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
		EpochNumber: fsm.epochNumber,
	}

	blockMetaHash, err := blockMeta.Hash(block.Hash())
	require.NoError(t, err)

	msg := runtime.BuildPrePrepareMessage(proposal, nil, &proto.View{})
	require.Equal(t, blockMetaHash.Bytes(), msg.GetPreprepareData().ProposalHash)

	mBlockBuilder.AssertExpectations(t)
}

func TestFSM_BuildProposal_WithCommitEpochTxGood(t *testing.T) {
	t.Parallel()

	const (
		accountCount             = 5
		committedCount           = 4
		parentCount              = 3
		confirmedStateSyncsCount = 5
		currentRound             = 0
		parentBlockNumber        = 1023
	)

	validators := validator.NewTestValidators(t, accountCount)
	extra := createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, accountCount-1, committedCount, parentCount)

	parent := &types.Header{Number: parentBlockNumber, ExtraData: extra}
	parent.ComputeHash()
	stateBlock := createDummyStateBlock(parentBlockNumber+1, parent.Hash, extra)

	mBlockBuilder := newBlockBuilderMock(stateBlock)
	mBlockBuilder.On("WriteTx", mock.Anything).Return(error(nil)).Once()

	blockChainMock := new(polychain.BlockchainMock)

	runtime := &consensusRuntime{
		logger: hclog.NewNullLogger(),
		config: &config.Runtime{
			Key: wallet.NewKey(validators.GetPrivateIdentities()[0]),
		},
	}

	fsm := &fsm{parent: parent,
		blockBuilder:     mBlockBuilder,
		config:           &config.PolyBFT{},
		blockchain:       blockChainMock,
		isEndOfEpoch:     true,
		validators:       validators.ToValidatorSet(),
		commitEpochInput: createTestCommitEpochInput(t, 0, 10),
		logger:           hclog.NewNullLogger(),
		forks:            &chain.Forks{chain.Governance: chain.NewFork(0)},
	}

	proposal, err := fsm.BuildProposal(currentRound)
	assert.NoError(t, err)
	assert.NotNil(t, proposal)

	block := types.Block{}
	require.NoError(t, block.UnmarshalRLP(proposal))

	assert.Equal(t, stateBlock.Block.MarshalRLP(), proposal)

	blockMeta := &polytypes.BlockMetaData{
		BlockRound:  currentRound,
		EpochNumber: fsm.epochNumber,
	}

	blockMetaHash, err := blockMeta.Hash(block.Hash())
	require.NoError(t, err)

	msg := runtime.BuildPrePrepareMessage(proposal, nil, &proto.View{})
	require.Equal(t, blockMetaHash.Bytes(), msg.GetPreprepareData().ProposalHash)

	mBlockBuilder.AssertExpectations(t)
}

func TestFSM_BuildProposal_EpochEndingBlock_FailedToApplyStateTx(t *testing.T) {
	t.Parallel()

	const (
		accountCount      = 5
		committedCount    = 4
		parentCount       = 3
		parentBlockNumber = 1023
	)

	validators := validator.NewTestValidators(t, accountCount)
	extra := createTestExtra(validators.GetPublicIdentities(), validator.AccountSet{}, accountCount-1, committedCount, parentCount)

	parent := &types.Header{Number: parentBlockNumber, ExtraData: extra}

	mBlockBuilder := new(polychain.BlockBuilderMock)
	mBlockBuilder.On("WriteTx", mock.Anything).Return(errors.New("error")).Once()
	mBlockBuilder.On("Reset").Return(error(nil)).Once()

	validatorSet := validator.NewValidatorSet(validators.GetPublicIdentities(), hclog.NewNullLogger())

	fsm := &fsm{parent: parent, blockBuilder: mBlockBuilder, blockchain: &polychain.BlockchainMock{},
		isEndOfEpoch:     true,
		validators:       validatorSet,
		commitEpochInput: createTestCommitEpochInput(t, 0, 10),
	}

	_, err := fsm.BuildProposal(0)
	assert.ErrorContains(t, err, "failed to apply commit epoch transaction")
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
	blockBuilderMock.On("WriteTx", mock.Anything).Return(error(nil)).Once()

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
		parent:             parent,
		blockBuilder:       blockBuilderMock,
		config:             &config.PolyBFT{},
		blockchain:         blockChainMock,
		isEndOfEpoch:       true,
		validators:         validatorSet,
		commitEpochInput:   createTestCommitEpochInput(t, 0, 10),
		logger:             hclog.NewNullLogger(),
		newValidatorsDelta: newDelta,
		forks:              &chain.Forks{chain.Governance: chain.NewFork(0)},
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

	fsm := &fsm{parent: parent,
		blockBuilder: blockBuilderMock,
		config:       &config.PolyBFT{},
		blockchain:   new(polychain.BlockchainMock),
		isEndOfEpoch: false,
		validators:   testValidators.ToValidatorSet(),
		logger:       hclog.NewNullLogger(),
		forks:        &chain.Forks{chain.Governance: chain.NewFork(0)},
	}

	proposal, err := fsm.BuildProposal(0)
	assert.NoError(t, err)
	assert.NotNil(t, proposal)

	blockExtra, err := polytypes.GetIbftExtra(stateBlock.Block.Header.ExtraData)
	assert.NoError(t, err)
	assert.Nil(t, blockExtra.Validators)

	blockBuilderMock.AssertExpectations(t)
}

func TestFSM_BuildProposal_EpochEndingBlock_FailToGetNextValidatorsHash(t *testing.T) {
	t.Parallel()

	const (
		accountCount      = 6
		signaturesCount   = 4
		parentBlockNumber = 49
	)

	testValidators := validator.NewTestValidators(t, accountCount)
	allAccounts := testValidators.GetPublicIdentities()
	extra := createTestExtraObject(allAccounts, validator.AccountSet{}, accountCount-1, signaturesCount, signaturesCount)
	extra.Validators = nil

	newValidatorDelta := &validator.ValidatorSetDelta{
		// this will prompt an error since all the validators are already in the validator set
		Added: testValidators.ToValidatorSet().Accounts(),
	}

	parent := &types.Header{Number: parentBlockNumber, ExtraData: extra.MarshalRLPTo(nil)}

	blockBuilderMock := new(polychain.BlockBuilderMock)
	blockBuilderMock.On("WriteTx", mock.Anything).Return(error(nil)).Once()
	blockBuilderMock.On("Reset").Return(error(nil)).Once()

	fsm := &fsm{parent: parent,
		blockBuilder:       blockBuilderMock,
		config:             &config.PolyBFT{},
		isEndOfEpoch:       true,
		validators:         testValidators.ToValidatorSet(),
		commitEpochInput:   createTestCommitEpochInput(t, 0, 10),
		newValidatorsDelta: newValidatorDelta,
		forks:              &chain.Forks{chain.Governance: chain.NewFork(0)},
	}

	proposal, err := fsm.BuildProposal(0)
	assert.ErrorContains(t, err, "already present in the validators snapshot")
	assert.Nil(t, proposal)

	blockBuilderMock.AssertNotCalled(t, "Build")
	blockBuilderMock.AssertExpectations(t)
}

func TestFSM_VerifyStateTransactions_CommitEpoch(t *testing.T) {
	t.Parallel()

	t.Run("commit epoch at end of epoch", func(t *testing.T) {
		t.Parallel()

		validators := validator.NewTestValidators(t, 5)
		validatorSet := validator.NewValidatorSet(validators.GetPublicIdentities(), hclog.NewNullLogger())

		fsm := &fsm{
			parent:                 &types.Header{Number: 9},
			isEndOfEpoch:           true,
			isEndOfSprint:          true,
			validators:             validatorSet,
			commitEpochInput:       createTestCommitEpochInput(t, 0, 10),
			distributeRewardsInput: createTestDistributeRewardsInput(t, 0, validators.GetPublicIdentities(), 10),
			logger:                 hclog.NewNullLogger(),
			forks:                  &chain.Forks{chain.Governance: chain.NewFork(0)},
		}

		// add commit epoch commitEpochTx to the end of transactions list
		commitEpochTx, err := fsm.createCommitEpochTx()
		require.NoError(t, err)

		err = fsm.VerifyStateTransactions([]*types.Transaction{commitEpochTx})
		require.NoError(t, err)
	})

	t.Run("Middle of epoch with transactions", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			parent:           &types.Header{Number: 5},
			commitEpochInput: createTestCommitEpochInput(t, 0, 10),
		}
		tx, err := fsm.createCommitEpochTx()
		require.NoError(t, err)
		err = fsm.VerifyStateTransactions([]*types.Transaction{tx})
		require.ErrorContains(t, err, errCommitEpochTxNotExpected.Error())
	})

	t.Run("Middle of epoch without transaction", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			parent:           &types.Header{Number: 5},
			commitEpochInput: createTestCommitEpochInput(t, 0, 10),
			forks:            &chain.Forks{chain.Governance: chain.NewFork(0)},
		}
		err := fsm.VerifyStateTransactions([]*types.Transaction{})
		require.NoError(t, err)
	})

	t.Run("End of epoch without transaction", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			parent:           &types.Header{Number: 9},
			isEndOfEpoch:     true,
			commitEpochInput: createTestCommitEpochInput(t, 0, 10),
			forks:            &chain.Forks{chain.Governance: chain.NewFork(0)},
		}
		err := fsm.VerifyStateTransactions([]*types.Transaction{})
		require.ErrorContains(t, err, errCommitEpochTxDoesNotExist.Error())
	})

	t.Run("End of epoch wrong commit transaction", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			isEndOfEpoch:     true,
			parent:           &types.Header{Number: 9},
			commitEpochInput: createTestCommitEpochInput(t, 0, 10),
		}
		commitEpochInput, err := createTestCommitEpochInput(t, 1, 5).EncodeAbi()
		require.NoError(t, err)

		commitEpochTx := createStateTransactionWithData(contracts.EpochManagerContract, commitEpochInput)
		err = fsm.VerifyStateTransactions([]*types.Transaction{commitEpochTx})
		require.ErrorContains(t, err, "invalid commit epoch transaction")
	})

	t.Run("end of epoch, more than one commit epoch transaction", func(t *testing.T) {
		t.Parallel()

		txs := make([]*types.Transaction, 2)
		fsm := &fsm{
			isEndOfEpoch:     true,
			parent:           &types.Header{Number: 9},
			commitEpochInput: createTestCommitEpochInput(t, 0, 10),
		}

		commitEpochTxOne, err := fsm.createCommitEpochTx()
		require.NoError(t, err)

		txs[0] = commitEpochTxOne

		commitEpochTxTwo := createTestCommitEpochInput(t, 0, 100)
		input, err := commitEpochTxTwo.EncodeAbi()
		require.NoError(t, err)

		txs[1] = createStateTransactionWithData(types.ZeroAddress, input)

		assert.ErrorIs(t, fsm.VerifyStateTransactions(txs), errCommitEpochTxSingleExpected)
	})
}
func TestFSM_VerifyStateTransactions_DistributeRewards(t *testing.T) {
	t.Parallel()

	validators := validator.NewTestValidators(t, 5)
	validatorSet := validator.NewValidatorSet(validators.GetPublicIdentities(), hclog.NewNullLogger())

	t.Run("distribute rewards on first block of epoch", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			parent:                 &types.Header{Number: 10},
			isFirstBlockOfEpoch:    true,
			validators:             validatorSet,
			distributeRewardsInput: createTestDistributeRewardsInput(t, 0, validators.GetPublicIdentities(), 10),
			logger:                 hclog.NewNullLogger(),
			forks:                  &chain.Forks{chain.Governance: chain.NewFork(0)},
		}

		distributeRewardsTx, err := fsm.createDistributeRewardsTx()
		require.NoError(t, err)

		err = fsm.VerifyStateTransactions([]*types.Transaction{distributeRewardsTx})
		require.NoError(t, err)
	})

	t.Run("not a first block of epoch", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			parent:                 &types.Header{Number: 9},
			validators:             validatorSet,
			distributeRewardsInput: createTestDistributeRewardsInput(t, 0, validators.GetPublicIdentities(), 10),
			logger:                 hclog.NewNullLogger(),
			forks:                  &chain.Forks{chain.Governance: chain.NewFork(0)},
		}

		distributeRewardsTx, err := fsm.createDistributeRewardsTx()
		require.NoError(t, err)

		err = fsm.VerifyStateTransactions([]*types.Transaction{distributeRewardsTx})
		require.ErrorContains(t, err, errDistributeRewardsTxNotExpected.Error())
	})

	t.Run("two distribute rewards txs", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			isFirstBlockOfEpoch:    true,
			parent:                 &types.Header{Number: 9},
			validators:             validatorSet,
			distributeRewardsInput: createTestDistributeRewardsInput(t, 0, validators.GetPublicIdentities(), 10),
			logger:                 hclog.NewNullLogger(),
			forks:                  &chain.Forks{chain.Governance: chain.NewFork(0)},
		}

		distributeRewardsTx, err := fsm.createDistributeRewardsTx()
		require.NoError(t, err)

		err = fsm.VerifyStateTransactions([]*types.Transaction{distributeRewardsTx, distributeRewardsTx})
		require.ErrorContains(t, err, errDistributeRewardsTxSingleExpected.Error())
	})

	t.Run("no distribute rewards tx", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{
			isFirstBlockOfEpoch: true,
			parent:              &types.Header{Number: 9},
			validators:          validatorSet,
			logger:              hclog.NewNullLogger(),
			forks:               &chain.Forks{chain.Governance: chain.NewFork(0)},
		}

		err := fsm.VerifyStateTransactions([]*types.Transaction{})
		require.ErrorContains(t, err, errDistributeRewardsTxDoesNotExist.Error())
	})
}

func TestFSM_VerifyStateTransaction_BridgeBatches(t *testing.T) {
	t.Parallel()

	t.Run("submit batches at end of sprint", func(t *testing.T) {
		t.Parallel()

		var (
			pendingBridgeBatches [2]*bridge.PendingBridgeBatch
			bridgeMessageEvents  [2][]*contractsapi.BridgeMsgEvent
			signedBridgeBatches  [2]*bridge.BridgeBatchSigned
		)

		validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E"})
		pendingBridgeBatches[0], signedBridgeBatches[0], bridgeMessageEvents[0] = bridge.BuildBridgeBatchAndBridgeEvents(t, 10, uint64(3), 2)
		pendingBridgeBatches[1], signedBridgeBatches[1], bridgeMessageEvents[1] = bridge.BuildBridgeBatchAndBridgeEvents(t, 10, uint64(3), 12)

		executeForValidators := func(aliases ...string) error {
			for _, sc := range signedBridgeBatches {
				// add register batches state transaction
				hash, err := sc.Hash()
				require.NoError(t, err)
				signature := createSignature(t, validators.GetPrivateIdentities(aliases...), hash, signer.DomainBridge)
				sc.AggSignature = *signature
			}

			f := &fsm{
				isEndOfSprint: true,
				parent:        &types.Header{Number: 9},
				validators:    validators.ToValidatorSet(),
				forks:         &chain.Forks{chain.Governance: chain.NewFork(0)},
			}

			var txns []*types.Transaction

			for i, sc := range signedBridgeBatches {
				inputData, err := sc.EncodeAbi()
				require.NoError(t, err)

				if i == 0 {
					tx := createStateTransactionWithData(types.StringToAddress("0x10101010101"), inputData)
					txns = append(txns, tx)
				}
			}

			return f.VerifyStateTransactions(txns)
		}

		assert.NoError(t, executeForValidators("A", "B", "C", "D"))
		assert.ErrorContains(t, executeForValidators("A", "B", "C"), "quorum size not reached for state tx")
	})

	t.Run("invalid signature", func(t *testing.T) {
		t.Parallel()

		validators := validator.NewTestValidators(t, 5)
		bridgeBatch := createTestBridgeBatch(t, validators.GetPrivateIdentities())
		nonValidators := validator.NewTestValidators(t, 3)
		aggregatedSigs := bls.Signatures{}

		nonValidators.IterAcct(nil, func(t *validator.TestValidator) {
			aggregatedSigs = append(aggregatedSigs, t.MustSign([]byte("dummyHash"), signer.DomainBridge))
		})

		sig, err := aggregatedSigs.Aggregate().Marshal()
		require.NoError(t, err)

		bridgeBatch.AggSignature.AggregatedSignature = sig

		validatorSet := validator.NewValidatorSet(validators.GetPublicIdentities(), hclog.NewNullLogger())

		fsm := &fsm{
			isEndOfSprint:                 true,
			parent:                        &types.Header{Number: 9},
			validators:                    validatorSet,
			proposerBridgeBatchToRegister: []*bridge.BridgeBatchSigned{bridgeBatch},
			logger:                        hclog.NewNullLogger(),
		}

		bridgeBatchTx, err := fsm.createBridgeBatchTx(bridgeBatch)
		require.NoError(t, err)

		err = fsm.VerifyStateTransactions([]*types.Transaction{bridgeBatchTx})
		require.ErrorContains(t, err, "invalid signature")
	})

	t.Run("quorum size not reached", func(t *testing.T) {
		t.Parallel()

		blsKey, err := bls.GenerateBlsKey()
		require.NoError(t, err)

		data := polytesting.GenerateRandomBytes(t)

		signature, err := blsKey.Sign(data, bridge.TestDomain)
		require.NoError(t, err)

		signatures := bls.Signatures{signature}

		aggSig, err := signatures.Aggregate().Marshal()
		require.NoError(t, err)

		validators := validator.NewTestValidators(t, 5)
		bridgeBatch := createTestBridgeBatch(t, validators.GetPrivateIdentities())
		bridgeBatch.AggSignature = polytypes.Signature{
			AggregatedSignature: aggSig,
			Bitmap:              []byte{},
		}

		validatorSet := validator.NewValidatorSet(validators.GetPublicIdentities(), hclog.NewNullLogger())

		fsm := &fsm{
			isEndOfEpoch:                  true,
			isEndOfSprint:                 true,
			parent:                        &types.Header{Number: 9},
			validators:                    validatorSet,
			proposerBridgeBatchToRegister: []*bridge.BridgeBatchSigned{bridgeBatch},
			commitEpochInput:              createTestCommitEpochInput(t, 0, 10),
			distributeRewardsInput:        createTestDistributeRewardsInput(t, 0, nil, 10),
			logger:                        hclog.NewNullLogger(),
		}

		bridgeBatchTx, err := fsm.createBridgeBatchTx(bridgeBatch)
		require.NoError(t, err)

		// add commit epoch commitEpochTx to the end of transactions list
		commitEpochTx, err := fsm.createCommitEpochTx()
		require.NoError(t, err)

		stateTxs := []*types.Transaction{commitEpochTx, bridgeBatchTx}

		err = fsm.VerifyStateTransactions(stateTxs)
		require.ErrorContains(t, err, "quorum size not reached")
	})

	t.Run("batch in unexpected block", func(t *testing.T) {
		t.Parallel()

		fsm := &fsm{}

		encodedBatch, err := bridge.CreateTestBridgeBatchMessage(t, 0, 0).EncodeAbi()
		require.NoError(t, err)

		tx := createStateTransactionWithData(contracts.BridgeStorageContract, encodedBatch)
		assert.ErrorContains(t, fsm.VerifyStateTransactions([]*types.Transaction{tx}),
			"bridge batch tx is not allowed in non-sprint block")
	})

	t.Run("two batch transactions", func(t *testing.T) {
		t.Parallel()

		validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E", "F"})
		_, bridgeBatchSigned, _ := bridge.BuildBridgeBatchAndBridgeEvents(t, 10, uint64(3), 2)

		validatorSet := validator.NewValidatorSet(validators.GetPublicIdentities(), hclog.NewNullLogger())

		f := &fsm{
			isEndOfSprint: true,
			validators:    validatorSet,
			parent:        &types.Header{Number: 9},
		}

		hash, err := bridgeBatchSigned.Hash()
		require.NoError(t, err)

		var txns []*types.Transaction

		signature := createSignature(t, validators.GetPrivateIdentities("A", "B", "C", "D", "E"), hash, signer.DomainBridge)
		bridgeBatchSigned.AggSignature = *signature

		inputData, err := bridgeBatchSigned.EncodeAbi()
		require.NoError(t, err)

		gatewayContractAddr := types.StringToAddress("0x10101010101")

		txns = append(txns,
			createStateTransactionWithData(gatewayContractAddr, inputData))
		inputData, err = bridgeBatchSigned.EncodeAbi()
		require.NoError(t, err)

		txns = append(txns,
			createStateTransactionWithData(gatewayContractAddr, inputData))
		err = f.VerifyStateTransactions(txns)
		require.ErrorContains(t, err, "only one bridge batch tx is allowed per block")
	})
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
		parent:       parent,
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		validators:   validators.ToValidatorSet(),
		logger:       hclog.NewNullLogger(),
		forks:        &chain.Forks{chain.Governance: chain.NewFork(0)},
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
		parent:       parent,
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		validators:   validators.ToValidatorSet(),
		logger:       hclog.NewNullLogger(),
		forks:        &chain.Forks{chain.Governance: chain.NewFork(0)},
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
		parent:       parent,
		blockBuilder: mBlockBuilder,
		config:       &config.PolyBFT{},
		blockchain:   &polychain.BlockchainMock{},
		validators:   validatorSet,
		logger:       hclog.NewNullLogger(),
		forks:        &chain.Forks{chain.Governance: chain.NewFork(0)},
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

	commitEpoch := createTestCommitEpochInput(t, 1, 10)
	commitEpochTxInput, err := commitEpoch.EncodeAbi()
	require.NoError(t, err)

	validatorsForInput := make([]*contractsapi.Validator, 0)

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

	for _, testValidator := range validators.Validators {
		validatorsForInput = append(validatorsForInput,
			&contractsapi.Validator{
				Address:     testValidator.Address(),
				VotingPower: new(big.Int).SetUint64(testValidator.VotingPower),
				BlsKey:      testValidator.ValidatorMetadata().BlsKey.ToBigInt()})
	}

	validatorsForInput = append(validatorsForInput,
		&contractsapi.Validator{
			Address:     types.BytesToAddress([]byte{0, 1, 2, 3}),
			VotingPower: big.NewInt(1),
			BlsKey:      privateKey.PublicKey().ToBigInt()})

	commitValidatorSet := createTestCommitValidatorSetBridgeStorageInput(t, validatorsForInput,
		[2]*big.Int{big.NewInt(1), big.NewInt(2)}, big.NewInt(1).Bytes())

	commitValidatorSetInput, err := commitValidatorSet.EncodeAbi()
	require.NoError(t, err)

	stateBlock.Block.Header.Hash = proposalHash
	stateBlock.Block.Header.ParentHash = parent.Hash
	stateBlock.Block.Header.Timestamp = uint64(time.Now().UTC().Unix())
	stateBlock.Block.Transactions = []*types.Transaction{
		createStateTransactionWithData(contracts.EpochManagerContract, commitEpochTxInput),
		createStateTransactionWithData(contracts.BridgeStorageContract, commitValidatorSetInput),
	}

	proposal := stateBlock.Block.MarshalRLP()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("ProcessBlock", mock.Anything, mock.Anything).
		Return(stateBlock, error(nil)).
		Maybe()

	fsm := &fsm{
		parent:             parent,
		blockchain:         blockchainMock,
		validators:         validators.ToValidatorSet(),
		logger:             hclog.NewNullLogger(),
		isEndOfEpoch:       true,
		commitEpochInput:   commitEpoch,
		polybftBackend:     polybftBackendMock,
		newValidatorsDelta: newValidatorDelta,
		config:             &config.PolyBFT{BlockTimeDrift: 1},
		forks:              &chain.Forks{chain.Governance: chain.NewFork(0)},
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
		parent:         parent,
		blockchain:     blockchainMock,
		validators:     validators.ToValidatorSet(),
		logger:         hclog.NewNullLogger(),
		polybftBackend: polybftBackendMock,
		config:         &config.PolyBFT{BlockTimeDrift: 1},
		forks:          &chain.Forks{chain.Governance: chain.NewFork(0)},
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
		parent:     parent,
		blockchain: &polychain.BlockchainMock{},
		validators: validators.ToValidatorSet(),
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
			parent:       parent,
			blockBuilder: mBlockBuilder,
			blockchain:   &polychain.BlockchainMock{},
			validators:   validators.ToValidatorSet(),
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
			parent:     parent,
			blockchain: &polychain.BlockchainMock{},
			validators: validators.ToValidatorSet(),
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
		parent:     parent,
		blockchain: &polychain.BlockchainMock{},
		validators: validators.ToValidatorSet(),
		logger:     hclog.NewNullLogger(),
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
			parent:       parent,
			blockBuilder: builderMock,
			target:       builtBlock,
			blockchain:   chainMock,
			validators:   validator.NewValidatorSet(validatorsMetadata[0:len(validatorsMetadata)-1], hclog.NewNullLogger()),
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

	fsm := &fsm{parent: parent, blockBuilder: mBlockBuilder, blockchain: &polychain.BlockchainMock{},
		validators: validatorSet,
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
	fsm := &fsm{parent: parent}
	assert.Equal(t, parentNumber+1, fsm.Height())
}

func TestFSM_DecodeBridgeBatchStateTxs(t *testing.T) {
	t.Parallel()

	const (
		from       = 15
		eventsSize = 40
	)

	_, signedBridgeBatch, _ := bridge.BuildBridgeBatchAndBridgeEvents(t, eventsSize, uint64(3), from)

	f := &fsm{
		proposerBridgeBatchToRegister: []*bridge.BridgeBatchSigned{signedBridgeBatch},
		commitEpochInput:              createTestCommitEpochInput(t, 0, 10),
		distributeRewardsInput:        createTestDistributeRewardsInput(t, 0, nil, 10),
		logger:                        hclog.NewNullLogger(),
		parent:                        &types.Header{},
	}

	bridgeBatchTx, err := f.createBridgeBatchTx(signedBridgeBatch)
	require.NoError(t, err)

	decodedData, err := decodeStateTransaction(bridgeBatchTx.Input())
	require.NoError(t, err)

	decodedBridgeBatchMsg, ok := decodedData.(*bridge.BridgeBatchSigned)
	require.True(t, ok)

	numberOfMessages := len(signedBridgeBatch.MessageBatch.Messages)

	require.Equal(t, signedBridgeBatch.MessageBatch.Messages[numberOfMessages-1].ID, decodedBridgeBatchMsg.MessageBatch.Messages[numberOfMessages-1].ID)
	require.Equal(t, signedBridgeBatch.AggSignature, decodedBridgeBatchMsg.AggSignature)
}

func TestFSM_DecodeCommitEpochStateTx(t *testing.T) {
	t.Parallel()

	commitEpoch := createTestCommitEpochInput(t, 0, 10)
	input, err := commitEpoch.EncodeAbi()
	require.NoError(t, err)
	require.NotNil(t, input)

	tx := createStateTransactionWithData(contracts.EpochManagerContract, input)
	decodedInputData, err := decodeStateTransaction(tx.Input())
	require.NoError(t, err)

	decodedCommitEpoch, ok := decodedInputData.(*contractsapi.CommitEpochEpochManagerFn)
	require.True(t, ok)
	require.True(t, commitEpoch.ID.Cmp(decodedCommitEpoch.ID) == 0)
	require.NotNil(t, decodedCommitEpoch.Epoch)
	require.True(t, commitEpoch.Epoch.StartBlock.Cmp(decodedCommitEpoch.Epoch.StartBlock) == 0)
	require.True(t, commitEpoch.Epoch.EndBlock.Cmp(decodedCommitEpoch.Epoch.EndBlock) == 0)
}

func TestFSM_VerifyStateTransaction_InvalidTypeOfStateTransactions(t *testing.T) {
	t.Parallel()

	f := &fsm{
		isEndOfSprint: true,
	}

	var txns []*types.Transaction
	txns = append(txns,
		createStateTransactionWithData(types.StringToAddress("0x1010101001"), []byte{9, 3, 1, 1}))

	require.ErrorContains(t, f.VerifyStateTransactions(txns), "unknown state transaction")
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
		parent:         parent,
		blockchain:     &polychain.BlockchainMock{},
		polybftBackend: polybftBackendMock,
		validators:     validatorSet,
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

func createTestBridgeBatch(t *testing.T, accounts []*wallet.Account) *bridge.BridgeBatchSigned {
	t.Helper()

	bitmap := bitmap.Bitmap{}
	bridgeMessageEvents := make([]*contractsapi.BridgeMsgEvent, len(accounts))

	for i := 0; i < len(accounts); i++ {
		bridgeMessageEvents[i] = &contractsapi.BridgeMsgEvent{
			ID:                 big.NewInt(int64(i)),
			Sender:             accounts[i].Ecdsa.Address(),
			Receiver:           accounts[0].Ecdsa.Address(),
			Data:               []byte{},
			SourceChainID:      big.NewInt(0),
			DestinationChainID: big.NewInt(1),
		}

		bitmap.Set(uint64(i))
	}

	newPendingBridgeBatch, err := bridge.NewPendingBridgeBatch(1, bridgeMessageEvents)
	require.NoError(t, err)

	hash, err := newPendingBridgeBatch.Hash()
	require.NoError(t, err)

	var signatures bls.Signatures

	for _, a := range accounts {
		signature, err := a.Bls.Sign(hash.Bytes(), signer.DomainBridge)
		assert.NoError(t, err)

		signatures = append(signatures, signature)
	}

	aggregatedSignature, err := signatures.Aggregate().Marshal()
	assert.NoError(t, err)

	signature := polytypes.Signature{
		AggregatedSignature: aggregatedSignature,
		Bitmap:              bitmap,
	}

	assert.NoError(t, err)

	return &bridge.BridgeBatchSigned{
		MessageBatch: newPendingBridgeBatch.BridgeMessageBatch,
		AggSignature: signature,
	}
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

func createTestCommitValidatorSetBridgeStorageInput(t *testing.T, validatorSet []*contractsapi.Validator,
	signature [2]*big.Int, bitmap []byte) *contractsapi.CommitValidatorSetBridgeStorageFn {
	t.Helper()

	commitValidatorSet := &contractsapi.CommitValidatorSetBridgeStorageFn{
		NewValidatorSet: validatorSet,
		Signature:       signature,
		Bitmap:          bitmap,
	}

	return commitValidatorSet
}
