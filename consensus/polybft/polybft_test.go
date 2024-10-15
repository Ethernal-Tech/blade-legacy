package polybft

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	polytesting "github.com/0xPolygon/polygon-edge/consensus/polybft/testing"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	vs "github.com/0xPolygon/polygon-edge/consensus/polybft/validator-snapshot"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/helper/progress"
	"github.com/0xPolygon/polygon-edge/txpool"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// the test initializes polybft and chain mock (map of headers) after which a new header is verified
// firstly, two invalid situation of header verifications are triggered (missing Committed field and invalid validators for ParentCommitted)
// afterwards, valid inclusion into the block chain is checked
// and at the end there is a situation when header is already a part of blockchain
func TestPolybft_VerifyHeader(t *testing.T) {
	t.Parallel()

	const (
		allValidatorsSize = 6 // overall there are 6 validators
		validatorSetSize  = 5 // only 5 validators are active at the time
		fixedEpochSize    = uint64(10)
	)

	updateHeaderExtra := func(header *types.Header,
		validators *validator.ValidatorSetDelta,
		parentSignature *polytypes.Signature,
		blockMeta *polytypes.BlockMetaData,
		committedAccounts []*wallet.Account) *polytypes.Signature {
		extra := &polytypes.Extra{
			Validators:    validators,
			Parent:        parentSignature,
			BlockMetaData: blockMeta,
			Committed:     &polytypes.Signature{},
		}

		if extra.BlockMetaData == nil {
			extra.BlockMetaData = &polytypes.BlockMetaData{}
		}

		header.ExtraData = extra.MarshalRLPTo(nil)
		header.ComputeHash()

		if len(committedAccounts) > 0 {
			blockMetaHash, err := extra.BlockMetaData.Hash(header.Hash)
			require.NoError(t, err)

			extra.Committed = createSignature(t, committedAccounts, blockMetaHash, signer.DomainBridge)
			header.ExtraData = extra.MarshalRLPTo(nil)
		}

		return extra.Committed
	}

	// create all validators
	validators := validator.NewTestValidators(t, allValidatorsSize)

	// create configuration
	polyBftConfig := config.PolyBFT{
		InitialValidatorSet: validators.GetParamValidators(),
		EpochSize:           fixedEpochSize,
		SprintSize:          5,
		BlockTimeDrift:      10,
	}

	validatorSet := validators.GetPublicIdentities()
	accounts := validators.GetPrivateIdentities()

	// calculate validators before and after the end of the first epoch
	validatorSetParent, validatorSetCurrent := validatorSet[:len(validatorSet)-1], validatorSet[1:]
	accountSetParent, accountSetCurrent := accounts[:len(accounts)-1], accounts[1:]

	// create header map to simulate blockchain
	headersMap := &polytesting.TestHeadersMap{}

	// create genesis header
	genesisDelta, err := validator.CreateValidatorSetDelta(nil, validatorSetParent)
	require.NoError(t, err)

	genesisHeader := &types.Header{Number: 0}
	updateHeaderExtra(genesisHeader, genesisDelta, nil, nil, nil)

	// add genesis header to map
	headersMap.AddHeader(genesisHeader)

	// create headers from 1 to 9
	for i := uint64(1); i < polyBftConfig.EpochSize; i++ {
		delta, err := validator.CreateValidatorSetDelta(validatorSetParent, validatorSetParent)
		require.NoError(t, err)

		header := &types.Header{Number: i}
		updateHeaderExtra(header, delta, nil, &polytypes.BlockMetaData{EpochNumber: 1}, nil)

		// add headers from 1 to 9 to map (blockchain imitation)
		headersMap.AddHeader(header)
	}

	// mock blockchain
	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)
	blockchainMock.On("GetHeaderByHash", mock.Anything).Return(headersMap.GetHeaderByHash)

	// create polybft with appropriate mocks
	validatorSnapCache, err := vs.NewValidatorsSnapshotCache(
		hclog.NewNullLogger(),
		state.NewTestState(t),
		blockchainMock,
	)
	require.NoError(t, err)

	polybft := &Polybft{
		closeCh:             make(chan struct{}),
		logger:              hclog.NewNullLogger(),
		genesisClientConfig: &polyBftConfig,
		blockchain:          blockchainMock,
		validatorsCache:     validatorSnapCache,
		runtime: &consensusRuntime{
			epoch: &epochMetadata{
				CurrentClientConfig: &polyBftConfig,
			},
		},
	}

	// create parent header (block 10)
	parentDelta, err := validator.CreateValidatorSetDelta(validatorSetParent, validatorSetCurrent)
	require.NoError(t, err)

	parentHeader := &types.Header{
		Number:    polyBftConfig.EpochSize,
		Timestamp: uint64(time.Now().UTC().Unix()),
	}
	parentCommitment := updateHeaderExtra(parentHeader, parentDelta, nil, &polytypes.BlockMetaData{EpochNumber: 1}, accountSetParent)

	// add parent header to map
	headersMap.AddHeader(parentHeader)

	// create current header (block 11) with all appropriate fields required for validation
	currentDelta, err := validator.CreateValidatorSetDelta(validatorSetCurrent, validatorSetCurrent)
	require.NoError(t, err)

	currentHeader := &types.Header{
		Number:     polyBftConfig.EpochSize + 1,
		ParentHash: parentHeader.Hash,
		Timestamp:  parentHeader.Timestamp + 1,
		MixHash:    polytypes.PolyBFTMixDigest,
		Difficulty: 1,
	}
	updateHeaderExtra(currentHeader, currentDelta, nil,
		&polytypes.BlockMetaData{
			EpochNumber: 2,
		}, nil)

	currentHeader.Hash[0]++
	assert.ErrorContains(t, polybft.VerifyHeader(currentHeader), "invalid header hash")

	// omit Parent field (parent signature) intentionally
	updateHeaderExtra(currentHeader, currentDelta, nil,
		&polytypes.BlockMetaData{
			EpochNumber: 1},
		accountSetCurrent)

	// since parent signature is intentionally disregarded the following error is expected
	assert.ErrorContains(t, polybft.VerifyHeader(currentHeader), "failed to verify signatures for parent of block")

	updateHeaderExtra(currentHeader, currentDelta, parentCommitment,
		&polytypes.BlockMetaData{
			EpochNumber: 1},
		accountSetCurrent)

	assert.NoError(t, polybft.VerifyHeader(currentHeader))

	validatorSnapCache, err = vs.NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	require.NoError(t, err)

	// clean validator snapshot cache (re-instantiate it), submit invalid validator set for parent signature and expect the following error
	polybft.validatorsCache = validatorSnapCache
	assert.NoError(t, polybft.validatorsCache.StoreSnapshot(
		&vs.ValidatorSnapshot{Epoch: 0, Snapshot: validatorSetCurrent}, nil)) // invalid validator set is submitted
	assert.NoError(t, polybft.validatorsCache.StoreSnapshot(
		&vs.ValidatorSnapshot{Epoch: 1, Snapshot: validatorSetCurrent}, nil))
	assert.ErrorContains(t, polybft.VerifyHeader(currentHeader), "failed to verify signatures for parent of block")

	// clean validators cache again and set valid snapshots
	validatorSnapCache, err = vs.NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	require.NoError(t, err)

	polybft.validatorsCache = validatorSnapCache
	assert.NoError(t, polybft.validatorsCache.StoreSnapshot(
		&vs.ValidatorSnapshot{Epoch: 0, Snapshot: validatorSetParent}, nil))
	assert.NoError(t, polybft.validatorsCache.StoreSnapshot(
		&vs.ValidatorSnapshot{Epoch: 1, Snapshot: validatorSetCurrent}, nil))
	assert.NoError(t, polybft.VerifyHeader(currentHeader))

	// add current header to the blockchain (headersMap) and try validating again
	headersMap.AddHeader(currentHeader)
	assert.NoError(t, polybft.VerifyHeader(currentHeader))
}

func TestPolybft_Close(t *testing.T) {
	t.Parallel()

	syncer := &polychain.SyncerMock{}
	syncer.On("Close", mock.Anything).Return(error(nil)).Once()

	polybft := Polybft{
		closeCh: make(chan struct{}),
		syncer:  syncer,
		runtime: &consensusRuntime{},
		state:   state.NewTestState(t),
	}

	assert.NoError(t, polybft.Close())

	<-polybft.closeCh

	syncer.AssertExpectations(t)

	errExpected := errors.New("something")
	syncer.On("Close", mock.Anything).Return(errExpected).Once()

	polybft.closeCh = make(chan struct{})

	assert.Error(t, errExpected, polybft.Close())

	select {
	case <-polybft.closeCh:
		assert.Fail(t, "channel closing is invoked")
	case <-time.After(time.Millisecond * 100):
	}

	syncer.AssertExpectations(t)
}

func TestPolybft_GetSyncProgression(t *testing.T) {
	t.Parallel()

	result := &progress.Progression{}

	syncer := &polychain.SyncerMock{}
	syncer.On("GetSyncProgression", mock.Anything).Return(result).Once()

	polybft := Polybft{
		syncer: syncer,
	}

	assert.Equal(t, result, polybft.GetSyncProgression())
}

func Test_Factory(t *testing.T) {
	t.Parallel()

	const epochSize = uint64(141)

	txPool := &txpool.TxPool{}

	params := &consensus.Params{
		TxPool: txPool,
		Logger: hclog.Default(),
		Config: &consensus.Config{
			Config: map[string]interface{}{
				"EpochSize": epochSize,
			},
		},
	}

	r, err := Factory(params)

	require.NoError(t, err)
	require.NotNil(t, r)

	polybft, ok := r.(*Polybft)
	require.True(t, ok)

	assert.Equal(t, txPool, polybft.txPool)
	assert.Equal(t, epochSize, polybft.genesisClientConfig.EpochSize)
	assert.Equal(t, params, polybft.config)
}

func Test_GenesisPostHookFactory(t *testing.T) {
	t.Parallel()

	const (
		epochSize     = 15
		maxValidators = 150
	)

	validators := validator.NewTestValidators(t, 6)
	admin := validators.ToValidatorSet().Accounts().GetAddresses()[0]
	bridgeCfg := createTestBridgeConfig()
	cases := []struct {
		name            string
		config          *config.PolyBFT
		bridgeAllowList *chain.AddressListConfig
		expectedErr     error
	}{
		{
			name: "access lists disabled",
			config: &config.PolyBFT{
				InitialValidatorSet: validators.GetParamValidators(),
				Bridge:              map[uint64]*config.Bridge{0: bridgeCfg},
				EpochSize:           epochSize,
				RewardConfig:        &config.Rewards{WalletAmount: ethgo.Ether(1000)},
				NativeTokenConfig:   &config.Token{Name: "Test", Symbol: "TEST", Decimals: 18},
				MaxValidatorSetSize: maxValidators,
				BladeAdmin:          admin,
				GovernanceConfig: &config.Governance{
					VotingDelay:              big.NewInt(0),
					VotingPeriod:             big.NewInt(10),
					ProposalThreshold:        big.NewInt(25),
					ProposalQuorumPercentage: 67,
				},
			},
		},
		{
			name: "access lists enabled",
			config: &config.PolyBFT{
				InitialValidatorSet: validators.GetParamValidators(),
				Bridge:              map[uint64]*config.Bridge{0: bridgeCfg},
				EpochSize:           epochSize,
				RewardConfig:        &config.Rewards{WalletAmount: ethgo.Ether(1000)},
				NativeTokenConfig:   &config.Token{Name: "Test Mintable", Symbol: "TEST_MNT", Decimals: 18},
				MaxValidatorSetSize: maxValidators,
				BladeAdmin:          admin,
				GovernanceConfig: &config.Governance{
					VotingDelay:              big.NewInt(0),
					VotingPeriod:             big.NewInt(10),
					ProposalThreshold:        big.NewInt(25),
					ProposalQuorumPercentage: 67,
				},
			},
			bridgeAllowList: &chain.AddressListConfig{
				AdminAddresses:   []types.Address{validators.Validators["0"].Address()},
				EnabledAddresses: []types.Address{validators.Validators["1"].Address()},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := &chain.Params{
				Engine:          map[string]interface{}{config.ConsensusName: tc.config},
				BridgeAllowList: tc.bridgeAllowList,
			}
			chainConfig := &chain.Chain{Params: params, Genesis: &chain.Genesis{Alloc: make(map[types.Address]*chain.GenesisAccount)}}
			initHandler := GenesisPostHookFactory(chainConfig, config.ConsensusName)
			require.NotNil(t, initHandler)

			transition := systemstate.NewTestTransition(t, nil)
			if tc.expectedErr == nil {
				require.NoError(t, initHandler(transition))
			} else {
				require.ErrorIs(t, initHandler(transition), tc.expectedErr)
			}
		})
	}
}

// createTestBridgeConfig creates test bridge configuration with hard-coded addresses
func createTestBridgeConfig() *config.Bridge {
	return &config.Bridge{
		ExternalGatewayAddr:                  types.StringToAddress("1"),
		ExternalERC20PredicateAddr:           types.StringToAddress("2"),
		ExternalMintableERC20PredicateAddr:   types.StringToAddress("3"),
		ExternalNativeERC20Addr:              types.StringToAddress("4"),
		ExternalERC721PredicateAddr:          types.StringToAddress("5"),
		ExternalMintableERC721PredicateAddr:  types.StringToAddress("6"),
		ExternalERC1155PredicateAddr:         types.StringToAddress("7"),
		ExternalMintableERC1155PredicateAddr: types.StringToAddress("8"),
		JSONRPCEndpoint:                      "http://localhost:8545",
	}
}

func createSignature(t *testing.T, accounts []*wallet.Account, hash types.Hash, domain []byte) *polytypes.Signature {
	t.Helper()

	var signatures bls.Signatures

	var bmp bitmap.Bitmap
	for i, x := range accounts {
		bmp.Set(uint64(i))

		src, err := x.Bls.Sign(hash[:], domain)
		require.NoError(t, err)

		signatures = append(signatures, src)
	}

	aggs, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	return &polytypes.Signature{AggregatedSignature: aggs, Bitmap: bmp}
}
