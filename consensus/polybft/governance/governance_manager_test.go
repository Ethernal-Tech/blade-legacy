package governance

import (
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	polycfg "github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/forkmanager"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestGovernanceManager_PostEpoch(t *testing.T) {
	t.Parallel()

	state := newTestState(t)
	governanceManager := &governanceManager{
		state:  state,
		logger: hclog.NewNullLogger(),
	}

	// insert some governance event
	baseFeeChangeDenomEvent := &contractsapi.NewBaseFeeChangeDenomEvent{BaseFeeChangeDenom: big.NewInt(100)}
	epochRewardEvent := &contractsapi.NewEpochRewardEvent{Reward: big.NewInt(10000)}

	require.NoError(t, state.insertGovernanceEvent(1, baseFeeChangeDenomEvent, nil))
	require.NoError(t, state.insertGovernanceEvent(1, epochRewardEvent, nil))

	// no initial config was saved, so we expect an error
	require.ErrorIs(t, governanceManager.PostEpoch(&polytypes.PostEpochRequest{
		NewEpochID:        2,
		FirstBlockOfEpoch: 21,
		Forks:             &chain.Forks{chain.Governance: chain.NewFork(0)},
	}),
		errClientConfigNotFound)

	params := &chain.Params{
		BaseFeeChangeDenom: 8,
		Engine:             map[string]interface{}{polycfg.ConsensusName: createTestPolybftConfig()},
	}

	// insert initial config
	require.NoError(t, state.insertClientConfig(params, nil))

	// PostEpoch will now update config with new epoch reward value
	require.NoError(t, governanceManager.PostEpoch(&polytypes.PostEpochRequest{
		NewEpochID:        2,
		FirstBlockOfEpoch: 21,
		Forks:             &chain.Forks{chain.Governance: chain.NewFork(0)},
	}))

	updatedConfig, err := state.getClientConfig(nil)
	require.NoError(t, err)
	require.Equal(t, baseFeeChangeDenomEvent.BaseFeeChangeDenom.Uint64(), updatedConfig.BaseFeeChangeDenom)

	pbftConfig, err := polycfg.GetPolyBFTConfig(updatedConfig)
	require.NoError(t, err)

	require.Equal(t, epochRewardEvent.Reward.Uint64(), pbftConfig.EpochReward)
}

func TestGovernanceManager_PostBlock(t *testing.T) {
	t.Parallel()

	genesisPolybftConfig := createTestPolybftConfig()

	t.Run("Has no events in block", func(t *testing.T) {
		t.Parallel()

		// no governance events in receipts
		req := &polytypes.PostBlockRequest{
			FullBlock: &types.FullBlock{Block: &types.Block{Header: &types.Header{Number: 5}},
				Receipts: []*types.Receipt{},
			},
			Epoch: 1,
			Forks: &chain.Forks{chain.Governance: chain.NewFork(0)},
		}

		blockchainMock := new(polychain.BlockchainMock)
		blockchainMock.On("CurrentHeader").Return(&types.Header{
			Number: 0,
		})

		chainParams := &chain.Params{Engine: map[string]interface{}{polycfg.ConsensusName: genesisPolybftConfig}}
		gm, err := NewGovernanceManager(chainParams,
			hclog.NewNullLogger(), state.NewTestState(t), blockchainMock, nil)
		require.NoError(t, err)

		require.NoError(t, gm.PostBlock(req))

		governanceManager := gm.(*governanceManager)
		eventsRaw, err := governanceManager.state.getNetworkParamsEvents(1, nil)
		require.NoError(t, err)
		require.Len(t, eventsRaw, 0)
	})

	t.Run("Has new fork", func(t *testing.T) {
		t.Parallel()

		var (
			newForkHash  = types.StringToHash("0xNewForkHash")
			newForkBlock = big.NewInt(5)
			newForkName  = "newFork"
		)

		req := &polytypes.PostBlockRequest{
			FullBlock: &types.FullBlock{Block: &types.Block{Header: &types.Header{Number: 5}}},
			Epoch:     1,
			Forks:     &chain.Forks{chain.Governance: chain.NewFork(0)},
		}

		blockchainMock := new(polychain.BlockchainMock)
		blockchainMock.On("CurrentHeader").Return(&types.Header{
			Number: 4,
		})

		chainParams := &chain.Params{Engine: map[string]interface{}{polycfg.ConsensusName: genesisPolybftConfig}}
		gm, err := NewGovernanceManager(chainParams,
			hclog.NewNullLogger(), state.NewTestState(t), blockchainMock, nil)
		require.NoError(t, err)

		governanceManager := gm.(*governanceManager)

		// this cheats that we have this fork in code
		governanceManager.allForksHashes[newForkHash] = newForkName

		require.NoError(t, governanceManager.state.insertGovernanceEvent(1,
			&contractsapi.NewFeatureEvent{
				Feature: newForkHash, Block: newForkBlock,
			}, nil))

		// new fork should not be registered and enabled before PostBlock
		require.False(t, forkmanager.GetInstance().IsForkEnabled(newForkName, newForkBlock.Uint64()))

		require.NoError(t, governanceManager.PostBlock(req))

		// new fork should be registered and enabled before PostBlock
		require.True(t, forkmanager.GetInstance().IsForkEnabled(newForkName, newForkBlock.Uint64()))
	})
}
