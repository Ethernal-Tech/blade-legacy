package epoch

import (
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/oracle"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/forkmanager"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func init() {
	// for tests
	forkmanager.GetInstance().RegisterFork(chain.Governance, nil)
	forkmanager.GetInstance().ActivateFork(chain.Governance, 0) //nolint:errcheck
}

func TestGetTransactions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                         string
		blockInfo                    oracle.NewBlockInfo
		setupMocks                   func(*polytypes.PolybftMock, *polychain.BlockchainMock) *types.Header
		expectedNumberOfTransactions int
	}{
		{
			name: "Not end of epoch or start of epoch",
			blockInfo: oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: false,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 7},
			},
			expectedNumberOfTransactions: 0,
		},
		{
			name: "End of epoch",
			blockInfo: oracle.NewBlockInfo{
				IsEndOfEpoch:        true,
				IsFirstBlockOfEpoch: false,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 9},
			},
			expectedNumberOfTransactions: 1,
		},
		{
			name: "First block of epoch",
			blockInfo: oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: true,
				EpochSize:           10,
				CurrentEpoch:        3,
				FirstBlockInEpoch:   21,
			},
			setupMocks: func(polybftMock *polytypes.PolybftMock, blockchainMock *polychain.BlockchainMock) *types.Header {
				validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E"})

				lastBuiltBlock, headerMap := polytypes.CreateTestBlocks(t, 20, 10, validators.GetPublicIdentities())

				blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headerMap.GetHeader)
				polybftMock.On("GetValidators", mock.Anything, mock.Anything).Return(validators.GetPublicIdentities()).Times(10)

				return lastBuiltBlock
			},
			expectedNumberOfTransactions: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			polybftMock := polytypes.NewPolybftMock(t)
			blockchainMock := new(polychain.BlockchainMock)

			var lastBuiltBlock *types.Header
			if tt.setupMocks != nil {
				lastBuiltBlock = tt.setupMocks(polybftMock, blockchainMock)
			}

			if lastBuiltBlock != nil {
				tt.blockInfo.ParentBlock = lastBuiltBlock
			}

			epochManager := NewEpochManager(polybftMock, blockchainMock)

			transactions, err := epochManager.GetTransactions(tt.blockInfo)
			require.NoError(t, err)
			require.Len(t, transactions, tt.expectedNumberOfTransactions)

			polybftMock.AssertExpectations(t)
			blockchainMock.AssertExpectations(t)
		})
	}
}
func TestVerifyTransactions(t *testing.T) {
	t.Parallel()

	validators := validator.NewTestValidatorsWithAliases(t, []string{"A", "B", "C", "D", "E"})

	t.Run("Valid commit epoch transaction", func(t *testing.T) {
		t.Parallel()

		blockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch: true,
			ParentBlock:  &types.Header{Number: 9},
			EpochSize:    10,
		}

		testTxn, err := createCommitEpochTx(blockInfo)
		require.NoError(t, err)

		epochManager := NewEpochManager(nil, nil)

		require.NoError(t, epochManager.VerifyTransactions(blockInfo, []*types.Transaction{testTxn}))
	})

	t.Run("Invalid commit epoch transaction", func(t *testing.T) {
		t.Parallel()

		correctBlockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch: true,
			ParentBlock:  &types.Header{Number: 9},
			EpochSize:    10,
		}

		incorrectBlockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch: true,
			ParentBlock:  &types.Header{Number: 9},
			EpochSize:    9,
		}

		testTxn, err := createCommitEpochTx(incorrectBlockInfo)
		require.NoError(t, err)

		epochManager := NewEpochManager(nil, nil)

		require.ErrorContains(t, epochManager.VerifyTransactions(correctBlockInfo, []*types.Transaction{testTxn}),
			"invalid commit epoch transaction")
	})

	t.Run("Duplicate commit epoch transaction", func(t *testing.T) {
		t.Parallel()

		blockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch: true,
			ParentBlock:  &types.Header{Number: 19},
			EpochSize:    10,
		}

		testTxn, err := createCommitEpochTx(blockInfo)
		require.NoError(t, err)

		epochManager := NewEpochManager(nil, nil)

		require.ErrorIs(t, epochManager.VerifyTransactions(blockInfo, []*types.Transaction{testTxn, testTxn}),
			errCommitEpochTxSingleExpected)
	})

	t.Run("Commit epoch transaction unexpected", func(t *testing.T) {
		t.Parallel()

		blockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch: true,
			ParentBlock:  &types.Header{Number: 29},
			EpochSize:    10,
		}

		testTxn, err := createCommitEpochTx(blockInfo)
		require.NoError(t, err)

		epochManager := NewEpochManager(nil, nil)

		require.ErrorIs(t, epochManager.VerifyTransactions(oracle.NewBlockInfo{IsEndOfEpoch: false},
			[]*types.Transaction{testTxn}), errCommitEpochTxNotExpected)
	})

	t.Run("No commit epoch transaction when expected", func(t *testing.T) {
		t.Parallel()

		blockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch: true,
			ParentBlock:  &types.Header{Number: 29},
			EpochSize:    10,
		}

		epochManager := NewEpochManager(nil, nil)

		require.ErrorIs(t, epochManager.VerifyTransactions(blockInfo, []*types.Transaction{}), errCommitEpochTxDoesNotExist)
	})

	t.Run("Valid distribute rewards transaction", func(t *testing.T) {
		t.Parallel()

		lastBuiltBlock, headerMap := polytypes.CreateTestBlocks(t, 20, 10, validators.GetPublicIdentities())

		blockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch:        false,
			ParentBlock:         lastBuiltBlock,
			EpochSize:           10,
			FirstBlockInEpoch:   lastBuiltBlock.Number + 1,
			IsFirstBlockOfEpoch: true,
			CurrentEpoch:        3,
		}

		polybftMock := polytypes.NewPolybftMock(t)
		blockchainMock := new(polychain.BlockchainMock)

		blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headerMap.GetHeader)
		polybftMock.On("GetValidators", mock.Anything, mock.Anything).Return(validators.GetPublicIdentities()).Times(20)

		epochManager := NewEpochManager(polybftMock, blockchainMock)

		testTxn, err := epochManager.createDistributeRewardsTx(blockInfo)
		require.NoError(t, err)

		require.NoError(t, epochManager.VerifyTransactions(blockInfo, []*types.Transaction{testTxn}))

		blockchainMock.AssertExpectations(t)
		polybftMock.AssertExpectations(t)
	})

	t.Run("Invalid distribute rewards transaction", func(t *testing.T) {
		t.Parallel()

		lastBuiltBlock, headerMap := polytypes.CreateTestBlocks(t, 30, 10, validators.GetPublicIdentities())

		correctBlockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch:        false,
			ParentBlock:         lastBuiltBlock,
			EpochSize:           10,
			FirstBlockInEpoch:   lastBuiltBlock.Number + 1,
			IsFirstBlockOfEpoch: true,
			CurrentEpoch:        4,
		}

		incorrectBlockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch:        false,
			ParentBlock:         lastBuiltBlock,
			EpochSize:           9,
			FirstBlockInEpoch:   lastBuiltBlock.Number + 1,
			IsFirstBlockOfEpoch: true,
			CurrentEpoch:        3, // this will make it fail
		}

		polybftMock := polytypes.NewPolybftMock(t)
		blockchainMock := new(polychain.BlockchainMock)

		blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headerMap.GetHeader)
		polybftMock.On("GetValidators", mock.Anything, mock.Anything).Return(validators.GetPublicIdentities()).Times(20)

		epochManager := NewEpochManager(polybftMock, blockchainMock)

		testTxn, err := epochManager.createDistributeRewardsTx(incorrectBlockInfo)
		require.NoError(t, err)

		require.ErrorContains(t, epochManager.VerifyTransactions(correctBlockInfo, []*types.Transaction{testTxn}),
			"invalid distribute rewards transaction")

		blockchainMock.AssertExpectations(t)
		polybftMock.AssertExpectations(t)
	})

	t.Run("Duplicate distribute rewards transaction", func(t *testing.T) {
		t.Parallel()

		lastBuiltBlock, headerMap := polytypes.CreateTestBlocks(t, 40, 20, validators.GetPublicIdentities())

		blockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch:        false,
			ParentBlock:         lastBuiltBlock,
			EpochSize:           20,
			FirstBlockInEpoch:   lastBuiltBlock.Number + 1,
			IsFirstBlockOfEpoch: true,
			CurrentEpoch:        3,
		}

		polybftMock := polytypes.NewPolybftMock(t)
		blockchainMock := new(polychain.BlockchainMock)

		blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headerMap.GetHeader)
		polybftMock.On("GetValidators", mock.Anything, mock.Anything).Return(validators.GetPublicIdentities()).Times(40)

		epochManager := NewEpochManager(polybftMock, blockchainMock)

		testTxn, err := epochManager.createDistributeRewardsTx(blockInfo)
		require.NoError(t, err)

		require.ErrorIs(t, epochManager.VerifyTransactions(blockInfo, []*types.Transaction{testTxn, testTxn}),
			errDistributeRewardsTxSingleExpected)

		blockchainMock.AssertExpectations(t)
		polybftMock.AssertExpectations(t)
	})

	t.Run("No distribute rewards transaction when expected", func(t *testing.T) {
		t.Parallel()

		blockInfo := oracle.NewBlockInfo{
			IsEndOfEpoch:        false,
			ParentBlock:         &types.Header{Number: 50},
			EpochSize:           10,
			FirstBlockInEpoch:   51,
			IsFirstBlockOfEpoch: true,
			CurrentEpoch:        6,
		}

		epochManager := NewEpochManager(nil, nil)

		require.ErrorIs(t, epochManager.VerifyTransactions(blockInfo, []*types.Transaction{}),
			errDistributeRewardsTxDoesNotExist)
	})

	t.Run("Distribute rewards transaction unexpected", func(t *testing.T) {
		t.Parallel()

		blockInfo := oracle.NewBlockInfo{
			IsFirstBlockOfEpoch: false,
			ParentBlock:         &types.Header{Number: 39},
		}

		testTxn := helpers.CreateStateTransactionWithData(contracts.EpochManagerContract,
			new(contractsapi.DistributeRewardForEpochManagerFn).Sig())

		epochManager := NewEpochManager(nil, nil)

		require.ErrorIs(t, epochManager.VerifyTransactions(blockInfo, []*types.Transaction{testTxn}),
			errDistributeRewardsTxNotExpected)
	})
}
