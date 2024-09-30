package polybft

import (
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"testing"
	"time"

	ibftproto "github.com/0xPolygon/go-ibft/messages/proto"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bridge"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/governance"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/proposer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/stake"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	polytesting "github.com/0xPolygon/polygon-edge/consensus/polybft/testing"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/forkmanager"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func init() {
	// for tests
	forkmanager.GetInstance().RegisterFork(chain.Governance, nil)
	forkmanager.GetInstance().ActivateFork(chain.Governance, 0) //nolint:errcheck
}

func TestConsensusRuntime_isFixedSizeOfEpochMet_NotReachedEnd(t *testing.T) {
	t.Parallel()

	// because of slashing, we can assume some epochs started at random numbers
	var cases = []struct {
		epochSize, firstBlockInEpoch, parentBlockNumber uint64
	}{
		{4, 1, 2},
		{5, 1, 3},
		{6, 0, 6},
		{7, 0, 4},
		{8, 0, 5},
		{9, 4, 9},
		{10, 7, 10},
		{10, 1, 1},
	}

	config := &config.Runtime{GenesisConfig: &config.PolyBFT{}}
	runtime := &consensusRuntime{
		config:         config,
		lastBuiltBlock: &types.Header{},
		epoch:          &epochMetadata{CurrentClientConfig: config.GenesisConfig},
	}

	for _, c := range cases {
		runtime.epoch.CurrentClientConfig.EpochSize = c.epochSize
		runtime.epoch.FirstBlockInEpoch = c.firstBlockInEpoch
		assert.False(
			t,
			runtime.isFixedSizeOfEpochMet(c.parentBlockNumber+1, runtime.epoch),
			fmt.Sprintf(
				"Not expected end of epoch for epoch size=%v and parent block number=%v",
				c.epochSize,
				c.parentBlockNumber),
		)
	}
}

func TestConsensusRuntime_isFixedSizeOfEpochMet_ReachedEnd(t *testing.T) {
	t.Parallel()

	// because of slashing, we can assume some epochs started at random numbers
	var cases = []struct {
		epochSize, firstBlockInEpoch, blockNumber uint64
	}{
		{4, 1, 4},
		{5, 1, 5},
		{6, 0, 5},
		{7, 0, 6},
		{8, 0, 7},
		{9, 4, 12},
		{10, 7, 16},
		{10, 1, 10},
	}

	config := &config.Runtime{GenesisConfig: &config.PolyBFT{}}
	runtime := &consensusRuntime{
		config: config,
		epoch:  &epochMetadata{CurrentClientConfig: config.GenesisConfig},
	}

	for _, c := range cases {
		runtime.epoch.CurrentClientConfig.EpochSize = c.epochSize
		runtime.epoch.FirstBlockInEpoch = c.firstBlockInEpoch
		assert.True(
			t,
			runtime.isFixedSizeOfEpochMet(c.blockNumber, runtime.epoch),
			fmt.Sprintf(
				"Not expected end of epoch for epoch size=%v and parent block number=%v",
				c.epochSize,
				c.blockNumber),
		)
	}
}

func TestConsensusRuntime_isFixedSizeOfSprintMet_NotReachedEnd(t *testing.T) {
	t.Parallel()

	// because of slashing, we can assume some epochs started at random numbers
	var cases = []struct {
		sprintSize, firstBlockInEpoch, blockNumber uint64
	}{
		{4, 1, 2},
		{5, 1, 3},
		{6, 0, 6},
		{7, 0, 4},
		{8, 0, 5},
		{9, 4, 9},
		{10, 7, 10},
		{10, 1, 1},
	}

	config := &config.Runtime{GenesisConfig: &config.PolyBFT{}}
	runtime := &consensusRuntime{
		config: config,
		epoch:  &epochMetadata{CurrentClientConfig: config.GenesisConfig},
	}

	for _, c := range cases {
		runtime.epoch.CurrentClientConfig.SprintSize = c.sprintSize
		runtime.epoch.FirstBlockInEpoch = c.firstBlockInEpoch
		assert.False(t,
			runtime.isFixedSizeOfSprintMet(c.blockNumber, runtime.epoch),
			fmt.Sprintf(
				"Not expected end of sprint for sprint size=%v and parent block number=%v",
				c.sprintSize,
				c.blockNumber),
		)
	}
}

func TestConsensusRuntime_isFixedSizeOfSprintMet_ReachedEnd(t *testing.T) {
	t.Parallel()

	// because of slashing, we can assume some epochs started at random numbers
	var cases = []struct {
		sprintSize, firstBlockInEpoch, blockNumber uint64
	}{
		{4, 1, 4},
		{5, 1, 5},
		{6, 0, 5},
		{7, 0, 6},
		{8, 0, 7},
		{9, 4, 12},
		{10, 7, 16},
		{10, 1, 10},
		{5, 1, 10},
		{3, 3, 5},
	}

	config := &config.Runtime{GenesisConfig: &config.PolyBFT{}}
	runtime := &consensusRuntime{
		config: config,
		epoch:  &epochMetadata{CurrentClientConfig: config.GenesisConfig},
	}

	for _, c := range cases {
		runtime.epoch.CurrentClientConfig.SprintSize = c.sprintSize
		runtime.epoch.FirstBlockInEpoch = c.firstBlockInEpoch
		assert.True(t,
			runtime.isFixedSizeOfSprintMet(c.blockNumber, runtime.epoch),
			fmt.Sprintf(
				"Not expected end of sprint for sprint size=%v and parent block number=%v",
				c.sprintSize,
				c.blockNumber),
		)
	}
}

func TestConsensusRuntime_OnBlockInserted_EndOfEpoch(t *testing.T) {
	t.Parallel()

	const (
		epochSize       = uint64(10)
		validatorsCount = 7
	)

	currentEpochNumber := getEpochNumber(t, epochSize, epochSize)
	validatorSet := validator.NewTestValidators(t, validatorsCount).GetPublicIdentities()
	header, headerMap := createTestBlocks(t, epochSize, epochSize, validatorSet)
	builtBlock := consensus.BuildBlock(consensus.BuildBlockParams{
		Header: header,
	})

	newEpochNumber := currentEpochNumber + 1
	systemStateMock := new(systemstate.SystemStateMock)
	systemStateMock.On("GetEpoch").Return(newEpochNumber).Once()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetStateProviderForBlock", mock.Anything).Return(new(systemstate.StateProviderMock)).Once()
	blockchainMock.On("GetSystemState", mock.Anything, mock.Anything).Return(systemStateMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headerMap.GetHeader)

	polybftBackendMock := new(polytypes.PolybftBackendMock)
	polybftBackendMock.On("GetValidatorsWithTx", mock.Anything, mock.Anything, mock.Anything).Return(validatorSet).Times(3)
	polybftBackendMock.On("SetBlockTime", mock.Anything).Once()

	txPoolMock := new(polychain.TxPoolMock)
	txPoolMock.On("ResetWithBlock", mock.Anything).Once()

	snapshot := proposer.NewProposerSnapshot(epochSize-1, validatorSet)
	polybftCfg := &config.PolyBFT{EpochSize: epochSize}
	config := &config.Runtime{
		GenesisConfig: &config.PolyBFT{
			EpochSize: epochSize,
		},
		ChainParams: &chain.Params{Engine: map[string]interface{}{config.ConsensusName: polybftCfg}},
	}
	st := state.NewTestState(t)

	require.NoError(t, st.InsertLastProcessedEventsBlock(builtBlock.Number()-1, nil))

	proposerCalculator, err := proposer.NewProposerCalculatorFromSnapshot(snapshot, config, st,
		polybftBackendMock, blockchainMock, hclog.NewNullLogger())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		proposerCalculator: proposerCalculator,
		logger:             hclog.NewNullLogger(),
		state:              st,
		config:             config,
		blockchain:         blockchainMock,
		backend:            polybftBackendMock,
		txPool:             txPoolMock,
		epoch: &epochMetadata{
			Number:              currentEpochNumber,
			FirstBlockInEpoch:   header.Number - epochSize + 1,
			CurrentClientConfig: config.GenesisConfig,
		},
		lastBuiltBlock: &types.Header{Number: header.Number - 1},
		stakeManager:   &stake.DummyStakeManager{},
		eventProvider:  state.NewEventProvider(blockchainMock),
		governanceManager: &governance.DummyGovernanceManager{
			GetClientConfigFn: func() (*chain.Params, error) {
				return config.ChainParams, nil
			}},
	}

	runtime.bridge = &bridge.DummyBridge{}

	runtime.OnBlockInserted(&types.FullBlock{Block: builtBlock})
	require.Equal(t, newEpochNumber, runtime.epoch.Number)

	blockchainMock.AssertExpectations(t)
	systemStateMock.AssertExpectations(t)
}

func TestConsensusRuntime_OnBlockInserted_MiddleOfEpoch(t *testing.T) {
	t.Parallel()

	const (
		epoch             = 2
		epochSize         = uint64(10)
		firstBlockInEpoch = epochSize + 1
		blockNumber       = epochSize + 2
	)

	header := &types.Header{Number: blockNumber}
	builtBlock := consensus.BuildBlock(consensus.BuildBlockParams{
		Header: header,
	})

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(builtBlock.Header, true).Once()

	polybftBackendMock := new(polytypes.PolybftBackendMock)
	polybftBackendMock.On("GetValidatorsWithTx", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	txPoolMock := new(polychain.TxPoolMock)
	txPoolMock.On("ResetWithHeaders", mock.Anything).Once()

	snapshot := proposer.NewProposerSnapshot(blockNumber, []*validator.ValidatorMetadata{})
	config := &config.Runtime{
		GenesisConfig: &config.PolyBFT{EpochSize: epochSize},
	}

	proposerCalculator, err := proposer.NewProposerCalculatorFromSnapshot(snapshot, config, state.NewTestState(t),
		polybftBackendMock, blockchainMock, hclog.NewNullLogger())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		lastBuiltBlock: header,
		blockchain:     blockchainMock,
		txPool:         txPoolMock,
		config:         config,
		epoch: &epochMetadata{
			Number:            epoch,
			FirstBlockInEpoch: firstBlockInEpoch,
		},
		logger:             hclog.NewNullLogger(),
		proposerCalculator: proposerCalculator,
	}
	runtime.OnBlockInserted(&types.FullBlock{Block: builtBlock})

	require.Equal(t, header.Number, runtime.lastBuiltBlock.Number)
}

func TestConsensusRuntime_FSM_NotInValidatorSet(t *testing.T) {
	t.Parallel()

	validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D"})

	snapshot := proposer.NewProposerSnapshot(1, nil)
	config := &config.Runtime{
		GenesisConfig: &config.PolyBFT{
			EpochSize: 1,
		},
		Key: polytesting.CreateTestKey(t),
	}

	proposerCalculator, err := proposer.NewProposerCalculatorFromSnapshot(snapshot, config, state.NewTestState(t),
		new(polytypes.PolybftBackendMock), new(polychain.BlockchainMock), hclog.NewNullLogger())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		proposerCalculator: proposerCalculator,
		config:             config,
		epoch: &epochMetadata{
			Number:     1,
			Validators: validators.GetPublicIdentities(),
		},
		lastBuiltBlock: &types.Header{},
	}
	runtime.setIsActiveValidator(true)

	assert.ErrorIs(t, runtime.FSM(), errNotAValidator)
}

func TestConsensusRuntime_FSM_NotEndOfEpoch_NotEndOfSprint(t *testing.T) {
	t.Parallel()

	extra := &polytypes.Extra{
		BlockMetaData: &polytypes.BlockMetaData{},
	}
	lastBlock := &types.Header{
		Number:    1,
		ExtraData: extra.MarshalRLPTo(nil),
	}

	validators := validator.NewTestValidators(t, 3)
	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("NewBlockBuilder", mock.Anything).Return(new(polychain.BlockBuilderMock), nil).Once()

	snapshot := proposer.NewProposerSnapshot(1, nil)
	config := &config.Runtime{
		GenesisConfig: &config.PolyBFT{
			EpochSize:  10,
			SprintSize: 5,
		},
		Key:   wallet.NewKey(validators.GetPrivateIdentities()[0]),
		Forks: chain.AllForksEnabled,
	}

	proposerCalculator, err := proposer.NewProposerCalculatorFromSnapshot(snapshot, config, state.NewTestState(t),
		new(polytypes.PolybftBackendMock), blockchainMock, hclog.NewNullLogger())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		proposerCalculator: proposerCalculator,
		logger:             hclog.NewNullLogger(),
		config:             config,
		epoch: &epochMetadata{
			Number:              1,
			Validators:          validators.GetPublicIdentities(),
			FirstBlockInEpoch:   1,
			CurrentClientConfig: config.GenesisConfig,
		},
		blockchain:     blockchainMock,
		lastBuiltBlock: lastBlock,
		state:          state.NewTestState(t),
		bridge:         &bridge.DummyBridge{},
	}
	runtime.setIsActiveValidator(true)

	require.NoError(t, runtime.FSM())

	assert.True(t, runtime.IsActiveValidator())
	assert.False(t, runtime.fsm.isEndOfEpoch)
	assert.False(t, runtime.fsm.isEndOfSprint)
	assert.Equal(t, lastBlock.Number, runtime.fsm.parent.Number)

	address := runtime.config.Key.Address()
	assert.True(t, runtime.fsm.ValidatorSet().Includes(address))

	assert.NotNil(t, runtime.fsm.blockBuilder)
	assert.NotNil(t, runtime.fsm.blockchain)

	blockchainMock.AssertExpectations(t)
}

func TestConsensusRuntime_FSM_EndOfEpoch_BuildCommitEpoch(t *testing.T) {
	t.Parallel()

	const (
		epoch             = 0
		epochSize         = uint64(10)
		sprintSize        = uint64(3)
		firstBlockInEpoch = uint64(1)
		fromIndex         = uint64(0)
		toIndex           = uint64(9)
	)

	validatorAccounts := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E", "F"})
	validators := validatorAccounts.GetPublicIdentities()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("NewBlockBuilder", mock.Anything).Return(new(polychain.BlockBuilderMock), nil).Once()

	state := state.NewTestState(t)

	config := &config.Runtime{
		GenesisConfig: &config.PolyBFT{
			EpochSize:  epochSize,
			SprintSize: sprintSize,
		},
		Key:   validatorAccounts.GetValidator("A").Key(),
		Forks: chain.AllForksEnabled,
	}

	metadata := &epochMetadata{
		Validators:          validators,
		Number:              epoch,
		FirstBlockInEpoch:   firstBlockInEpoch,
		CurrentClientConfig: config.GenesisConfig,
	}

	snapshot := proposer.NewProposerSnapshot(1, nil)
	proposerCalculator, err := proposer.NewProposerCalculatorFromSnapshot(snapshot, config, state,
		new(polytypes.PolybftBackendMock), blockchainMock, hclog.NewNullLogger())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		proposerCalculator: proposerCalculator,
		logger:             hclog.NewNullLogger(),
		state:              state,
		epoch:              metadata,
		config:             config,
		lastBuiltBlock:     &types.Header{Number: 9},
		stakeManager:       &stake.DummyStakeManager{},
		bridge:             &bridge.DummyBridge{},
		blockchain:         blockchainMock,
	}

	assert.NoError(t, runtime.FSM())

	fsm := runtime.fsm
	assert.True(t, fsm.isEndOfEpoch)
	assert.NotNil(t, fsm.commitEpochInput)
	assert.NotEmpty(t, fsm.commitEpochInput)

	blockchainMock.AssertExpectations(t)
}

func Test_NewConsensusRuntime(t *testing.T) {
	t.Parallel()

	_, err := os.Create("/tmp/consensusState.db")
	require.NoError(t, err)

	polyBftConfig := &config.PolyBFT{
		/* 		Bridge: map[uint64]*BridgeConfig{0: {
			StateSenderAddr:       types.Address{0x13},
			CheckpointManagerAddr: types.Address{0x10},
			JSONRPCEndpoint:       "testEndpoint",
		}}, */
		EpochSize:  10,
		SprintSize: 10,
		BlockTime:  common.Duration{Duration: 2 * time.Second},
	}

	validators := validator.NewTestValidators(t, 3).GetPublicIdentities()

	systemStateMock := new(systemstate.SystemStateMock)
	systemStateMock.On("GetEpoch").Return(uint64(1)).Once()
	systemStateMock.On("GetNextCommittedIndex").Return(uint64(1)).Once()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("CurrentHeader").Return(&types.Header{Number: 1, ExtraData: createTestExtraForAccounts(t, 1, validators, nil)})
	blockchainMock.On("GetStateProviderForBlock", mock.Anything).Return(new(systemstate.StateProviderMock)).Once()
	blockchainMock.On("GetSystemState", mock.Anything, mock.Anything).Return(systemStateMock).Once()
	blockchainMock.On("GetHeaderByNumber", uint64(0)).Return(&types.Header{Number: 0, ExtraData: createTestExtraForAccounts(t, 0, validators, nil)})
	blockchainMock.On("GetHeaderByNumber", uint64(1)).Return(&types.Header{Number: 1, ExtraData: createTestExtraForAccounts(t, 1, validators, nil)})

	polybftBackendMock := new(polytypes.PolybftBackendMock)
	polybftBackendMock.On("GetValidatorsWithTx", mock.Anything, mock.Anything, mock.Anything).Return(validators).Times(4)
	polybftBackendMock.On("SetBlockTime", mock.Anything).Once()

	tmpDir := t.TempDir()
	st := state.NewTestState(t)

	config := &config.Runtime{
		ChainParams:   &chain.Params{Engine: map[string]interface{}{config.ConsensusName: polyBftConfig}},
		GenesisConfig: polyBftConfig,
		StateDataDir:  tmpDir,
		Key:           polytesting.CreateTestKey(t),
		EventTracker:  &consensus.EventTracker{},
		Forks:         chain.AllForksEnabled,
	}

	runtime, err := newConsensusRuntime(hclog.NewNullLogger(), config, st, polybftBackendMock, blockchainMock, nil, &mockTopic{})
	require.NoError(t, err)

	assert.False(t, runtime.IsActiveValidator())
	assert.Equal(t, runtime.config.StateDataDir, tmpDir)
	assert.Equal(t, uint64(10), runtime.config.GenesisConfig.SprintSize)
	assert.Equal(t, uint64(10), runtime.config.GenesisConfig.EpochSize)
	assert.Equal(t, "0x0000000000000000000000000000000000000101", contracts.EpochManagerContract.String())
	blockchainMock.AssertExpectations(t)
	polybftBackendMock.AssertExpectations(t)
}

func TestConsensusRuntime_restartEpoch_SameEpochNumberAsTheLastOne(t *testing.T) {
	t.Parallel()

	const originalBlockNumber = uint64(5)

	newCurrentHeader := &types.Header{Number: originalBlockNumber + 1}
	validatorSet := validator.NewTestValidators(t, 3).GetPublicIdentities()

	systemStateMock := new(systemstate.SystemStateMock)
	systemStateMock.On("GetEpoch").Return(uint64(1), nil).Once()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetStateProviderForBlock", mock.Anything).Return(new(systemstate.StateProviderMock)).Once()
	blockchainMock.On("GetSystemState", mock.Anything, mock.Anything).Return(systemStateMock).Once()

	snapshot := proposer.NewProposerSnapshot(1, nil)
	config := &config.Runtime{}

	proposerCalculator, err := proposer.NewProposerCalculatorFromSnapshot(snapshot, config, state.NewTestState(t),
		new(polytypes.PolybftBackendMock), blockchainMock, hclog.NewNullLogger())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		proposerCalculator: proposerCalculator,
		config:             config,
		epoch: &epochMetadata{
			Number:            1,
			Validators:        validatorSet,
			FirstBlockInEpoch: 1,
		},
		lastBuiltBlock: &types.Header{
			Number: originalBlockNumber,
		},
		blockchain: blockchainMock,
	}
	runtime.setIsActiveValidator(true)

	epoch, err := runtime.restartEpoch(newCurrentHeader, nil)

	require.NoError(t, err)

	for _, a := range validatorSet.GetAddresses() {
		assert.True(t, epoch.Validators.ContainsAddress(a))
	}

	systemStateMock.AssertExpectations(t)
	blockchainMock.AssertExpectations(t)
}

func TestConsensusRuntime_calculateCommitEpochInput_SecondEpoch(t *testing.T) {
	t.Parallel()

	const (
		currentEpoch           = 3
		epochSize              = 10
		currentEpochStartBlock = 21
		currentEpochEndBlock   = 30
		sprintSize             = 5
	)

	validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E"})
	polybftConfig := &config.PolyBFT{
		EpochSize:  epochSize,
		SprintSize: sprintSize,
	}

	lastBuiltBlock, headerMap := createTestBlocks(t, 20, epochSize, validators.GetPublicIdentities())

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headerMap.GetHeader)

	polybftBackendMock := new(polytypes.PolybftBackendMock)
	polybftBackendMock.On("GetValidators", mock.Anything, mock.Anything).Return(validators.GetPublicIdentities()).Times(10)

	config := &config.Runtime{
		GenesisConfig: polybftConfig,
		Key:           validators.GetValidator("A").Key(),
		Forks:         chain.AllForksEnabled,
	}

	consensusRuntime := &consensusRuntime{
		config:     config,
		blockchain: blockchainMock,
		backend:    polybftBackendMock,
		epoch: &epochMetadata{
			Number:            currentEpoch,
			Validators:        validators.GetPublicIdentities(),
			FirstBlockInEpoch: currentEpochStartBlock,
		},
		lastBuiltBlock: lastBuiltBlock,
	}

	distributeRewardsInput, err := consensusRuntime.calculateDistributeRewardsInput(
		true, false,
		lastBuiltBlock.Number+1,
		lastBuiltBlock, consensusRuntime.epoch.Number)
	assert.NoError(t, err)
	assert.Equal(t, uint64(currentEpoch-1), distributeRewardsInput.EpochID.Uint64())

	blockchainMock.AssertExpectations(t)
	polybftBackendMock.AssertExpectations(t)
}

func TestConsensusRuntime_IsValidValidator_BasicCases(t *testing.T) {
	t.Parallel()

	setupFn := func(t *testing.T) (*consensusRuntime, *validator.TestValidators) {
		t.Helper()

		validatorAccounts := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E", "F"})
		epoch := &epochMetadata{
			Validators: validatorAccounts.GetPublicIdentities("A", "B", "C", "D"),
		}
		runtime := &consensusRuntime{
			epoch:  epoch,
			logger: hclog.NewNullLogger(),
			fsm:    &fsm{validators: validator.NewValidatorSet(epoch.Validators, hclog.NewNullLogger())},
		}

		return runtime, validatorAccounts
	}

	cases := []struct {
		name          string
		signerAlias   string
		senderAlias   string
		isValidSender bool
	}{
		{
			name:          "Valid sender",
			signerAlias:   "A",
			senderAlias:   "A",
			isValidSender: true,
		},
		{
			name:          "Sender not amongst current validators",
			signerAlias:   "F",
			senderAlias:   "F",
			isValidSender: false,
		},
		{
			name:          "Sender and signer accounts mismatch",
			signerAlias:   "A",
			senderAlias:   "B",
			isValidSender: false,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			runtime, validatorAccounts := setupFn(t)
			signer := validatorAccounts.GetValidator(c.signerAlias)
			sender := validatorAccounts.GetValidator(c.senderAlias)
			msg, err := signer.Key().SignIBFTMessage(&ibftproto.IbftMessage{From: sender.Address().Bytes()})

			require.NoError(t, err)
			require.Equal(t, c.isValidSender, runtime.IsValidValidator(msg))
		})
	}
}

func TestConsensusRuntime_IsValidValidator_TamperSignature(t *testing.T) {
	t.Parallel()

	validatorAccounts := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E", "F"})
	epoch := &epochMetadata{
		Validators: validatorAccounts.GetPublicIdentities("A", "B", "C", "D"),
	}
	runtime := &consensusRuntime{
		epoch:  epoch,
		logger: hclog.NewNullLogger(),
		fsm:    &fsm{validators: validator.NewValidatorSet(epoch.Validators, hclog.NewNullLogger())},
	}

	// provide invalid signature
	sender := validatorAccounts.GetValidator("A")
	msg := &ibftproto.IbftMessage{
		From:      sender.Address().Bytes(),
		Signature: []byte{1, 2, 3, 4, 5},
	}
	require.False(t, runtime.IsValidValidator(msg))
}

func TestConsensusRuntime_TamperMessageContent(t *testing.T) {
	t.Parallel()

	validatorAccounts := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E", "F"})
	epoch := &epochMetadata{
		Validators: validatorAccounts.GetPublicIdentities("A", "B", "C", "D"),
	}
	runtime := &consensusRuntime{
		epoch:  epoch,
		logger: hclog.NewNullLogger(),
		fsm:    &fsm{validators: validator.NewValidatorSet(epoch.Validators, hclog.NewNullLogger())},
	}
	sender := validatorAccounts.GetValidator("A")
	proposalHash := []byte{2, 4, 6, 8, 10}
	proposalSignature, err := sender.Key().SignWithDomain(proposalHash, signer.DomainBridge)
	require.NoError(t, err)

	msg := &ibftproto.IbftMessage{
		View: &ibftproto.View{},
		From: sender.Address().Bytes(),
		Type: ibftproto.MessageType_COMMIT,
		Payload: &ibftproto.IbftMessage_CommitData{
			CommitData: &ibftproto.CommitMessage{
				ProposalHash:  proposalHash,
				CommittedSeal: proposalSignature,
			},
		},
	}
	// sign the message itself
	msg, err = sender.Key().SignIBFTMessage(msg)
	assert.NoError(t, err)
	// signature verification works
	assert.True(t, runtime.IsValidValidator(msg))

	// modify message without signing it again
	msg.Payload = &ibftproto.IbftMessage_CommitData{
		CommitData: &ibftproto.CommitMessage{
			ProposalHash:  []byte{1, 3, 5, 7, 9}, // modification
			CommittedSeal: proposalSignature,
		},
	}
	// signature isn't valid, because message was tampered
	assert.False(t, runtime.IsValidValidator(msg))
}

func TestConsensusRuntime_IsValidProposalHash(t *testing.T) {
	t.Parallel()

	extra := &polytypes.Extra{
		BlockMetaData: &polytypes.BlockMetaData{
			EpochNumber: 1,
			BlockRound:  1,
		},
	}
	block := &types.Block{
		Header: &types.Header{
			Number:    10,
			ExtraData: extra.MarshalRLPTo(nil),
		},
	}
	block.Header.ComputeHash()

	proposalHash, err := extra.BlockMetaData.Hash(block.Hash())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		logger:     hclog.NewNullLogger(),
		config:     &config.Runtime{},
		blockchain: new(polychain.BlockchainMock),
	}

	require.True(t, runtime.IsValidProposalHash(&ibftproto.Proposal{RawProposal: block.MarshalRLP()}, proposalHash.Bytes()))
}

func TestConsensusRuntime_IsValidProposalHash_InvalidProposalHash(t *testing.T) {
	t.Parallel()

	extra := &polytypes.Extra{
		BlockMetaData: &polytypes.BlockMetaData{
			EpochNumber: 1,
			BlockRound:  1,
		},
	}

	block := &types.Block{
		Header: &types.Header{
			Number:    10,
			ExtraData: extra.MarshalRLPTo(nil),
		},
	}

	proposalHash, err := extra.BlockMetaData.Hash(block.Hash())
	require.NoError(t, err)

	extra.BlockMetaData.BlockRound = 2 // change it so it is not the same as in proposal hash
	block.Header.ExtraData = extra.MarshalRLPTo(nil)
	block.Header.ComputeHash()

	runtime := &consensusRuntime{
		logger:     hclog.NewNullLogger(),
		config:     &config.Runtime{},
		blockchain: new(polychain.BlockchainMock),
	}

	require.False(t, runtime.IsValidProposalHash(&ibftproto.Proposal{RawProposal: block.MarshalRLP()}, proposalHash.Bytes()))
}

func TestConsensusRuntime_IsValidProposalHash_InvalidExtra(t *testing.T) {
	t.Parallel()

	extra := &polytypes.Extra{
		BlockMetaData: &polytypes.BlockMetaData{
			EpochNumber: 1,
			BlockRound:  1,
		},
	}

	block := &types.Block{
		Header: &types.Header{
			Number:    10,
			ExtraData: []byte{1, 2, 3}, // invalid extra in block
		},
	}
	block.Header.ComputeHash()

	proposalHash, err := extra.BlockMetaData.Hash(block.Hash())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		logger:     hclog.NewNullLogger(),
		config:     &config.Runtime{},
		blockchain: new(polychain.BlockchainMock),
	}

	require.False(t, runtime.IsValidProposalHash(&ibftproto.Proposal{RawProposal: block.MarshalRLP()}, proposalHash.Bytes()))
}

func TestConsensusRuntime_BuildProposal_InvalidParent(t *testing.T) {
	config := &config.Runtime{}
	snapshot := proposer.NewProposerSnapshot(1, nil)

	proposerCalculator, err := proposer.NewProposerCalculatorFromSnapshot(snapshot, config, state.NewTestState(t),
		new(polytypes.PolybftBackendMock), new(polychain.BlockchainMock), hclog.NewNullLogger())
	require.NoError(t, err)

	runtime := &consensusRuntime{
		logger:             hclog.NewNullLogger(),
		lastBuiltBlock:     &types.Header{Number: 2},
		epoch:              &epochMetadata{Number: 1},
		config:             config,
		proposerCalculator: proposerCalculator,
	}

	require.Nil(t, runtime.BuildProposal(&ibftproto.View{Round: 5}))
}

func TestConsensusRuntime_ID(t *testing.T) {
	t.Parallel()

	key1, key2 := polytesting.CreateTestKey(t), polytesting.CreateTestKey(t)
	runtime := &consensusRuntime{
		config: &config.Runtime{Key: key1},
	}

	require.Equal(t, runtime.ID(), key1.Address().Bytes())
	require.NotEqual(t, runtime.ID(), key2.Address().Bytes())
}

func TestConsensusRuntime_GetVotingPowers(t *testing.T) {
	t.Parallel()

	const height = 100

	validatorAccounts := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C"}, []uint64{1, 3, 2})
	runtime := &consensusRuntime{}

	_, err := runtime.GetVotingPowers(height)
	require.Error(t, err)

	runtime.fsm = &fsm{
		validators: validatorAccounts.ToValidatorSet(),
		parent:     &types.Header{Number: height},
	}

	_, err = runtime.GetVotingPowers(height)
	require.Error(t, err)

	runtime.fsm.parent.Number = height - 1

	val, err := runtime.GetVotingPowers(height)
	require.NoError(t, err)

	addresses := validatorAccounts.GetPublicIdentities([]string{"A", "B", "C"}...).GetAddresses()

	assert.Equal(t, uint64(1), val[types.AddressToString(addresses[0])].Uint64())
	assert.Equal(t, uint64(3), val[types.AddressToString(addresses[1])].Uint64())
	assert.Equal(t, uint64(2), val[types.AddressToString(addresses[2])].Uint64())
}

func TestConsensusRuntime_BuildRoundChangeMessage(t *testing.T) {
	t.Parallel()

	key := polytesting.CreateTestKey(t)
	view, rawProposal, certificate := &ibftproto.View{}, []byte{1}, &ibftproto.PreparedCertificate{}

	runtime := &consensusRuntime{
		config: &config.Runtime{
			Key: key,
		},
		logger: hclog.NewNullLogger(),
	}

	proposal := &ibftproto.Proposal{
		RawProposal: rawProposal,
		Round:       view.Round,
	}

	expected := ibftproto.IbftMessage{
		View: view,
		From: key.Address().Bytes(),
		Type: ibftproto.MessageType_ROUND_CHANGE,
		Payload: &ibftproto.IbftMessage_RoundChangeData{RoundChangeData: &ibftproto.RoundChangeMessage{
			LatestPreparedCertificate: certificate,
			LastPreparedProposal:      proposal,
		}},
	}

	signedMsg, err := key.SignIBFTMessage(&expected)
	require.NoError(t, err)

	assert.Equal(t, signedMsg, runtime.BuildRoundChangeMessage(proposal, certificate, view))
}

func TestConsensusRuntime_BuildCommitMessage(t *testing.T) {
	t.Parallel()

	key := polytesting.CreateTestKey(t)
	view, proposalHash := &ibftproto.View{}, []byte{1, 2, 4}

	runtime := &consensusRuntime{
		config: &config.Runtime{
			Key: key,
		},
	}

	committedSeal, err := key.SignWithDomain(proposalHash, signer.DomainBridge)
	require.NoError(t, err)

	expected := ibftproto.IbftMessage{
		View: view,
		From: key.Address().Bytes(),
		Type: ibftproto.MessageType_COMMIT,
		Payload: &ibftproto.IbftMessage_CommitData{
			CommitData: &ibftproto.CommitMessage{
				ProposalHash:  proposalHash,
				CommittedSeal: committedSeal,
			},
		},
	}

	signedMsg, err := key.SignIBFTMessage(&expected)
	require.NoError(t, err)

	assert.Equal(t, signedMsg, runtime.BuildCommitMessage(proposalHash, view))
}

func TestConsensusRuntime_BuildPrePrepareMessage_EmptyProposal(t *testing.T) {
	t.Parallel()

	runtime := &consensusRuntime{logger: hclog.NewNullLogger()}

	assert.Nil(t, runtime.BuildPrePrepareMessage(nil, &ibftproto.RoundChangeCertificate{},
		&ibftproto.View{Height: 1, Round: 0}))
}

func TestConsensusRuntime_IsValidProposalHash_EmptyProposal(t *testing.T) {
	t.Parallel()

	runtime := &consensusRuntime{logger: hclog.NewNullLogger()}

	assert.False(t, runtime.IsValidProposalHash(&ibftproto.Proposal{}, []byte("hash")))
}

func TestConsensusRuntime_BuildPrepareMessage(t *testing.T) {
	t.Parallel()

	key := polytesting.CreateTestKey(t)
	view, proposalHash := &ibftproto.View{}, []byte{1, 2, 4}

	runtime := &consensusRuntime{
		config: &config.Runtime{
			Key: key,
		},
		logger: hclog.NewNullLogger(),
	}

	expected := ibftproto.IbftMessage{
		View: view,
		From: key.Address().Bytes(),
		Type: ibftproto.MessageType_PREPARE,
		Payload: &ibftproto.IbftMessage_PrepareData{
			PrepareData: &ibftproto.PrepareMessage{
				ProposalHash: proposalHash,
			},
		},
	}

	signedMsg, err := key.SignIBFTMessage(&expected)
	require.NoError(t, err)

	assert.Equal(t, signedMsg, runtime.BuildPrepareMessage(proposalHash, view))
}

func TestConsensusRuntime_RoundStarts(t *testing.T) {
	cases := []struct {
		funcName string
		round    uint64
	}{
		{
			funcName: "ClearProposed",
			round:    0,
		},
		{
			funcName: "ReinsertProposed",
			round:    1,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.funcName, func(t *testing.T) {
			txPool := new(polychain.TxPoolMock)
			txPool.On(c.funcName).Once()

			runtime := &consensusRuntime{
				config: &config.Runtime{},
				logger: hclog.NewNullLogger(),
				txPool: txPool,
			}

			view := &ibftproto.View{Round: c.round}
			require.NoError(t, runtime.RoundStarts(view))
			txPool.AssertExpectations(t)
		})
	}
}

func TestConsensusRuntime_SequenceCancelled(t *testing.T) {
	txPool := new(polychain.TxPoolMock)
	txPool.On("ReinsertProposed").Once()

	runtime := &consensusRuntime{
		config: &config.Runtime{},
		logger: hclog.NewNullLogger(),
		txPool: txPool,
	}

	view := &ibftproto.View{}
	require.NoError(t, runtime.SequenceCancelled(view))
	txPool.AssertExpectations(t)
}

func createTestBlocks(t *testing.T, numberOfBlocks, defaultEpochSize uint64,
	validatorSet validator.AccountSet) (*types.Header, *polytesting.TestHeadersMap) {
	t.Helper()

	headerMap := &polytesting.TestHeadersMap{}
	bitmaps := createTestBitmaps(t, validatorSet, numberOfBlocks)

	extra := &polytypes.Extra{
		BlockMetaData: &polytypes.BlockMetaData{EpochNumber: 0},
	}

	genesisBlock := &types.Header{
		Number:    0,
		ExtraData: extra.MarshalRLPTo(nil),
	}
	parentHash := types.BytesToHash(big.NewInt(0).Bytes())

	headerMap.AddHeader(genesisBlock)

	var hash types.Hash

	var blockHeader *types.Header

	for i := uint64(1); i <= numberOfBlocks; i++ {
		big := big.NewInt(int64(i))
		hash = types.BytesToHash(big.Bytes())

		header := &types.Header{
			Number:     i,
			ParentHash: parentHash,
			ExtraData:  createTestExtraForAccounts(t, getEpochNumber(t, i, defaultEpochSize), validatorSet, bitmaps[i]),
			GasLimit:   types.StateTransactionGasLimit,
		}

		headerMap.AddHeader(header)

		parentHash = hash
		blockHeader = header
	}

	return blockHeader, headerMap
}

func createTestBitmaps(t *testing.T, validators validator.AccountSet, numberOfBlocks uint64) map[uint64]bitmap.Bitmap {
	t.Helper()

	bitmaps := make(map[uint64]bitmap.Bitmap, numberOfBlocks)

	rand.Seed(time.Now().UTC().Unix())

	for i := numberOfBlocks; i > 1; i-- {
		bitmap := bitmap.Bitmap{}
		j := 0

		for j != 3 {
			validator := validators[rand.Intn(validators.Len())]
			index := uint64(validators.Index(validator.Address))

			if !bitmap.IsSet(index) {
				bitmap.Set(index)

				j++
			}
		}

		bitmaps[i] = bitmap
	}

	return bitmaps
}

func createTestExtraForAccounts(t *testing.T, epoch uint64, validators validator.AccountSet, b bitmap.Bitmap) []byte {
	t.Helper()

	dummySignature := [64]byte{}
	extraData := polytypes.Extra{
		Validators: &validator.ValidatorSetDelta{
			Added:   validators,
			Removed: bitmap.Bitmap{},
		},
		Parent:        &polytypes.Signature{Bitmap: b, AggregatedSignature: dummySignature[:]},
		Committed:     &polytypes.Signature{Bitmap: b, AggregatedSignature: dummySignature[:]},
		BlockMetaData: &polytypes.BlockMetaData{EpochNumber: epoch},
	}

	return extraData.MarshalRLPTo(nil)
}

var _ bridge.Topic = &mockTopic{}

type mockTopic struct {
	published proto.Message
}

func (m *mockTopic) Publish(obj proto.Message) error {
	m.published = obj

	return nil
}

func (m *mockTopic) Subscribe(handler func(obj interface{}, from peer.ID)) error {
	return nil
}

// getEpochNumber returns epoch number for given blockNumber and epochSize.
// Epoch number is derived as a result of division of block number and epoch size.
// Since epoch number is 1-based (0 block represents special case zero epoch),
// we are incrementing result by one for non epoch-ending blocks.
func getEpochNumber(t *testing.T, blockNumber, epochSize uint64) uint64 {
	t.Helper()

	if blockNumber%epochSize == 0 { // is end of period
		return blockNumber / epochSize
	}

	return blockNumber/epochSize + 1
}
