package polybft

import (
	"math"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/types"
)

func TestIntegration_CommitEpoch(t *testing.T) {
	t.Parallel()

	// init validator sets
	validatorSetSize := []int{5, 10, 50, 100}

	initialBalance := uint64(5 * math.Pow(10, 18)) // 5 tokens
	reward := uint64(math.Pow(10, 18))             // 1 token
	walletAddress := types.StringToAddress("1234889893")

	validatorSets := make([]*validator.TestValidators, len(validatorSetSize))

	// create all validator sets which will be used in test
	for i, size := range validatorSetSize {
		aliases := make([]string, size)
		vps := make([]uint64, size)

		for j := 0; j < size; j++ {
			aliases[j] = "v" + strconv.Itoa(j)
			vps[j] = initialBalance
		}

		validatorSets[i] = validator.NewTestValidatorsWithAliases(t, aliases, vps)
	}

	// iterate through the validator set and do the test for each of them
	for _, currentValidators := range validatorSets {
		accSet := currentValidators.GetPublicIdentities()

		// validator data for polybft config
		initValidators := make([]*validator.GenesisValidator, accSet.Len())
		// add contracts to genesis data
		alloc := map[types.Address]*chain.GenesisAccount{
			contracts.NetworkParamsContract: {
				Code: contractsapi.NetworkParams.DeployedBytecode,
			},
			contracts.EpochManagerContract: {
				Code: contractsapi.EpochManager.DeployedBytecode,
			},
			contracts.NativeERC20TokenContract: {
				Code: contractsapi.NativeERC20Mintable.DeployedBytecode,
			},
			contracts.StakeManagerContract: {
				Code: contractsapi.StakeManager.DeployedBytecode,
			},
			walletAddress: {
				Balance: new(big.Int).SetUint64(initialBalance),
			},
		}

		for i, val := range accSet {
			// add validator to genesis data
			alloc[val.Address] = &chain.GenesisAccount{
				Balance: val.VotingPower,
			}

			// create validator data for polybft config
			initValidators[i] = &validator.GenesisValidator{
				Address: val.Address,
				Stake:   val.VotingPower,
				BlsKey:  hex.EncodeToString(val.BlsKey.Marshal()),
			}
		}

		polyBFTConfig := config.PolyBFT{
			InitialValidatorSet:  initValidators,
			EpochSize:            24 * 60 * 60 / 2,
			SprintSize:           5,
			EpochReward:          reward,
			BlockTime:            common.Duration{Duration: time.Second},
			MinValidatorSetSize:  4,
			MaxValidatorSetSize:  100,
			CheckpointInterval:   900,
			WithdrawalWaitPeriod: 1,
			BlockTimeDrift:       10,
			BladeAdmin:           accSet.GetAddresses()[0],
			// use 1st account as governance address
			Governance: currentValidators.ToValidatorSet().Accounts().GetAddresses()[0],
			RewardConfig: &config.Rewards{
				TokenAddress:  contracts.NativeERC20TokenContract,
				WalletAddress: walletAddress,
				WalletAmount:  new(big.Int).SetUint64(initialBalance),
			},
			GovernanceConfig: &config.Governance{
				VotingDelay:              big.NewInt(10),
				VotingPeriod:             big.NewInt(10),
				ProposalThreshold:        big.NewInt(25),
				ProposalQuorumPercentage: 67,
				ChildGovernorAddr:        contracts.ChildGovernorContract,
				ChildTimelockAddr:        contracts.ChildTimelockContract,
				NetworkParamsAddr:        contracts.NetworkParamsContract,
				ForkParamsAddr:           contracts.ForkParamsContract,
			},
			StakeTokenAddr: contracts.NativeERC20TokenContract,
		}

		transition := systemstate.NewTestTransition(t, alloc)

		// init NetworkParams
		require.NoError(t, initNetworkParamsContract(2, polyBFTConfig, transition))

		// init StakeManager
		require.NoError(t, initStakeManager(polyBFTConfig, transition))

		// init EpochManager
		require.NoError(t, initEpochManager(polyBFTConfig, transition))

		// approve EpochManager as reward token spender
		require.NoError(t, approveEpochManagerAsSpender(polyBFTConfig, transition))

		// create input for commit epoch
		commitEpoch := createTestCommitEpochInput(t, 1, polyBFTConfig.EpochSize)
		input, err := commitEpoch.EncodeAbi()
		require.NoError(t, err)

		// call commit epoch
		result := transition.Call2(contracts.SystemCaller, contracts.EpochManagerContract, input, big.NewInt(0), 10000000000)
		require.NoError(t, result.Err)
		t.Logf("Number of validators %d on commit epoch, Gas used %+v\n", accSet.Len(), result.GasUsed)

		commitEpoch = createTestCommitEpochInput(t, 2, polyBFTConfig.EpochSize)
		input, err = commitEpoch.EncodeAbi()
		require.NoError(t, err)

		// call commit epoch
		result = transition.Call2(contracts.SystemCaller, contracts.EpochManagerContract, input, big.NewInt(0), 10000000000)
		require.NoError(t, result.Err)
		t.Logf("Number of validators %d on commit epoch, Gas used %+v\n", accSet.Len(), result.GasUsed)
	}
}

func createTestCommitEpochInput(t *testing.T, epochID uint64,
	epochSize uint64) *contractsapi.CommitEpochEpochManagerFn {
	t.Helper()

	var startBlock uint64 = 0
	if epochID > 1 {
		startBlock = (epochID - 1) * epochSize
	}

	commitEpoch := &contractsapi.CommitEpochEpochManagerFn{
		ID: new(big.Int).SetUint64(epochID),
		Epoch: &contractsapi.Epoch{
			StartBlock: new(big.Int).SetUint64(startBlock + 1),
			EndBlock:   new(big.Int).SetUint64(epochSize * epochID),
			EpochRoot:  types.Hash{},
		},
		EpochSize: new(big.Int).SetUint64(epochSize),
	}

	return commitEpoch
}
