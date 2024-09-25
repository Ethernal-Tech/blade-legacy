package governance

import (
	"fmt"
	"math/big"
	"os"
	"path"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

// newTestState creates new instance of state used by tests.
func newTestState(tb testing.TB) *GovernanceStore {
	tb.Helper()

	dir := fmt.Sprintf("/tmp/consensus-temp_%v", time.Now().UTC().Format(time.RFC3339Nano))
	err := os.Mkdir(dir, 0775)

	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			tb.Fatal(err)
		}
	})

	db, err := bolt.Open(path.Join(dir, "my.db"), 0666, nil)
	if err != nil {
		tb.Fatal(err)
	}

	governanceStore, err := newGovernanceStore(db)
	if err != nil {
		tb.Fatal(err)
	}

	return governanceStore
}

func TestGovernanceStore_InsertAndGetEvents(t *testing.T) {
	t.Parallel()

	epoch := uint64(11)
	state := newTestState(t)

	// NetworkParams events
	checkpointIntervalEvent := &contractsapi.NewCheckpointBlockIntervalEvent{CheckpointInterval: big.NewInt(900)}
	epochSizeEvent := &contractsapi.NewEpochSizeEvent{Size: big.NewInt(10)}
	epochRewardEvent := &contractsapi.NewEpochRewardEvent{Reward: big.NewInt(1000)}
	minValidatorSetSizeEvent := &contractsapi.NewMinValidatorSetSizeEvent{MinValidatorSet: big.NewInt(4)}
	maxValidatorSetSizeEvent := &contractsapi.NewMaxValidatorSetSizeEvent{MaxValidatorSet: big.NewInt(100)}
	withdrawalPeriodEvent := &contractsapi.NewWithdrawalWaitPeriodEvent{WithdrawalPeriod: big.NewInt(1)}
	blockTimeEvent := &contractsapi.NewBlockTimeEvent{BlockTime: big.NewInt(2)}
	blockTimeDriftEvent := &contractsapi.NewBlockTimeDriftEvent{BlockTimeDrift: big.NewInt(10)}
	votingDelayEvent := &contractsapi.NewVotingDelayEvent{VotingDelay: big.NewInt(1000)}
	votingPeriodEvent := &contractsapi.NewVotingPeriodEvent{VotingPeriod: big.NewInt(10_000)}
	proposalThresholdEvent := &contractsapi.NewProposalThresholdEvent{ProposalThreshold: big.NewInt(1000)}
	sprintSizeEvent := &contractsapi.NewSprintSizeEvent{Size: big.NewInt(7)}
	// ForkParams events
	newFeatureEvent := &contractsapi.NewFeatureEvent{Feature: types.BytesToHash([]byte("OxSomeFeature1")),
		Block: big.NewInt(100_000)}
	updateFeatureEvent := &contractsapi.UpdatedFeatureEvent{Feature: types.BytesToHash([]byte("OxSomeFeature2")),
		Block: big.NewInt(150_000)}

	networkParamsEvents := []contractsapi.EventAbi{
		checkpointIntervalEvent,
		epochSizeEvent,
		epochRewardEvent,
		minValidatorSetSizeEvent,
		maxValidatorSetSizeEvent,
		withdrawalPeriodEvent,
		blockTimeEvent,
		blockTimeDriftEvent,
		votingDelayEvent,
		votingPeriodEvent,
		proposalThresholdEvent,
	}

	forkParamsEvents := []contractsapi.EventAbi{newFeatureEvent, updateFeatureEvent}

	allEvents := make([]contractsapi.EventAbi, 0)
	allEvents = append(allEvents, networkParamsEvents...)
	allEvents = append(allEvents, forkParamsEvents...)

	for _, e := range allEvents {
		require.NoError(t, state.insertGovernanceEvent(epoch, e, nil))
	}

	// test for an epoch that didn't have any events
	eventsRaw, err := state.getNetworkParamsEvents(10, nil)
	require.NoError(t, err)
	require.Len(t, eventsRaw, 0)

	// fork events are not saved per epoch so we should have 2
	forksInDB, err := state.getAllForkEvents(nil)
	require.NoError(t, err)
	require.Len(t, forksInDB, len(forkParamsEvents))

	// test for the epoch that had events
	eventsRaw, err = state.getNetworkParamsEvents(epoch, nil)
	require.NoError(t, err)
	require.Len(t, eventsRaw, len(networkParamsEvents))

	forksInDB, err = state.getAllForkEvents(nil)
	require.NoError(t, err)
	require.Len(t, forksInDB, len(forkParamsEvents))

	// insert some more events for current epoch
	newFeatureEventTwo := &contractsapi.UpdatedFeatureEvent{Feature: types.BytesToHash([]byte("OxSomeFeature3")),
		Block: big.NewInt(130_000)}

	require.NoError(t, state.insertGovernanceEvent(epoch, sprintSizeEvent, nil))
	require.NoError(t, state.insertGovernanceEvent(epoch, newFeatureEventTwo, nil))

	eventsRaw, err = state.getNetworkParamsEvents(epoch, nil)
	require.NoError(t, err)
	require.Len(t, eventsRaw, len(networkParamsEvents)+1)

	forksInDB, err = state.getAllForkEvents(nil)
	require.NoError(t, err)
	require.Len(t, forksInDB, len(forkParamsEvents)+1)
}

func TestGovernanceStore_InsertAndGetClientConfig(t *testing.T) {
	t.Parallel()

	initialPolyConfig := createTestPolybftConfig()
	initialConfig := &chain.Params{
		Engine:             map[string]interface{}{config.ConsensusName: initialPolyConfig},
		BaseFeeChangeDenom: 16,
	}
	state := newTestState(t)

	// try get config when there is none
	_, err := state.getClientConfig(nil)
	require.ErrorIs(t, err, errClientConfigNotFound)

	// insert config
	require.NoError(t, state.insertClientConfig(initialConfig, nil))

	// now config should exist
	configFromDB, err := state.getClientConfig(nil)
	require.NoError(t, err)

	polyConfigFromDB, err := config.GetPolyBFTConfig(configFromDB)
	require.NoError(t, err)

	// check some fields to make sure they are as expected
	require.Len(t, polyConfigFromDB.InitialValidatorSet, len(initialPolyConfig.InitialValidatorSet))
	require.Equal(t, polyConfigFromDB.BlockTime, initialPolyConfig.BlockTime)
	require.Equal(t, polyConfigFromDB.BlockTimeDrift, initialPolyConfig.BlockTimeDrift)
	require.Equal(t, polyConfigFromDB.CheckpointInterval, initialPolyConfig.CheckpointInterval)
	require.Equal(t, polyConfigFromDB.EpochReward, initialPolyConfig.EpochReward)
	require.Equal(t, polyConfigFromDB.EpochSize, initialPolyConfig.EpochSize)
	require.Equal(t, polyConfigFromDB.Governance, initialPolyConfig.Governance)
	require.Equal(t, configFromDB.BaseFeeChangeDenom, initialConfig.BaseFeeChangeDenom)
}

func createTestPolybftConfig() *config.PolyBFT {
	return &config.PolyBFT{
		InitialValidatorSet: []*validator.GenesisValidator{
			{
				Address: types.BytesToAddress([]byte{0, 1, 2}),
				Stake:   big.NewInt(100),
			},
			{
				Address: types.BytesToAddress([]byte{3, 4, 5}),
				Stake:   big.NewInt(100),
			},
			{
				Address: types.BytesToAddress([]byte{6, 7, 8}),
				Stake:   big.NewInt(100),
			},
			{
				Address: types.BytesToAddress([]byte{9, 10, 11}),
				Stake:   big.NewInt(100),
			},
		},
		Bridge: map[uint64]*config.Bridge{0: {
			ExternalGatewayAddr:                  types.StringToAddress("0xGatewayAddr"),
			ExternalERC20PredicateAddr:           types.StringToAddress("0xRootERC20PredicateAddr"),
			ExternalMintableERC20PredicateAddr:   types.StringToAddress("0xChildMintableERC20PredicateAddr"),
			ExternalERC721PredicateAddr:          types.StringToAddress("0xRootERC721PredicateAddr"),
			ExternalMintableERC721PredicateAddr:  types.StringToAddress("0xChildMintableERC721PredicateAddr"),
			ExternalERC1155PredicateAddr:         types.StringToAddress("0xRootERC1155PredicateAddr"),
			ExternalMintableERC1155PredicateAddr: types.StringToAddress("0xChildMintableERC1155PredicateAddr"),
			ExternalERC20Addr:                    types.StringToAddress("0xChildERC20Addr"),
			ExternalERC721Addr:                   types.StringToAddress("0xChildERC721Addr"),
			ExternalERC1155Addr:                  types.StringToAddress("0xChildERC1155Addr"),
			BLSAddress:                           types.StringToAddress("0xBLSAddress"),
			BN256G2Address:                       types.StringToAddress("0xBN256G2Address"),
			JSONRPCEndpoint:                      "http://mumbai-rpc.com",
			EventTrackerStartBlocks: map[types.Address]uint64{
				types.StringToAddress("SomeRootAddress"): 365_000,
			}},
		},
		EpochSize:           10,
		EpochReward:         1000,
		SprintSize:          5,
		BlockTime:           common.Duration{Duration: 2 * time.Second},
		MinValidatorSetSize: 4,
		MaxValidatorSetSize: 100,
		CheckpointInterval:  900,
		BlockTimeDrift:      10,
		Governance:          types.ZeroAddress,
		NativeTokenConfig: &config.Token{
			Name:     "Polygon_MATIC",
			Symbol:   "MATIC",
			Decimals: 18,
		},
		InitialTrieRoot:      types.ZeroHash,
		WithdrawalWaitPeriod: 1,
		RewardConfig: &config.Rewards{
			TokenAddress:  types.StringToAddress("0xRewardTokenAddr"),
			WalletAddress: types.StringToAddress("0xRewardWalletAddr"),
			WalletAmount:  big.NewInt(1_000_000),
		},
		GovernanceConfig: &config.Governance{
			VotingDelay:              big.NewInt(1000),
			VotingPeriod:             big.NewInt(10_0000),
			ProposalThreshold:        big.NewInt(1000),
			ProposalQuorumPercentage: 67,
			ChildGovernorAddr:        contracts.ChildGovernorContract,
			ChildTimelockAddr:        contracts.ChildTimelockContract,
			NetworkParamsAddr:        contracts.NetworkParamsContract,
			ForkParamsAddr:           contracts.ForkParamsContract,
		},
	}
}
