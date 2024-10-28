package bridge

import (
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/oracle"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetTransactions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                         string
		blockInfo                    *oracle.NewBlockInfo
		expectedNumberOfTransactions int
		expectedError                string
		setupMocks                   func(bridge *bridge, blockInfo *oracle.NewBlockInfo)
	}{
		{
			name: "first block of epoch, but start of the chain",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: true,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 0}, // genesis block
			},
			expectedNumberOfTransactions: 0,
		},
		{
			name: "first block of epoch, getting extra fails",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: true,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 10},
			},
			expectedError: "failed to get parent extra data",
		},
		{
			name: "first block of epoch, validator set delta empty",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: true,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 20},
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) {
				extra := polytypes.Extra{
					Validators: nil, // empty validator set delta
					BlockMetaData: &polytypes.BlockMetaData{
						EpochNumber: 2,
						BlockRound:  1,
					},
				}

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)
			},
			expectedNumberOfTransactions: 0,
		},
		{
			name: "first block of epoch, validator set delta not empty",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: true,
				CurrentEpoch:        7,
				EpochSize:           5,
				ParentBlock:         &types.Header{Number: 30},
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) {
				extra, validators, _ := createAndSignExtra(t, 5, 1, blockInfo.CurrentEpoch)

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)
				blockInfo.CurrentEpochValidatorSet = validators.ToValidatorSet()
			},
			expectedNumberOfTransactions: 1,
		},
		{
			name: "not end of sprint or first block of epoch",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: false,
				IsEndOfSprint:       false,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 38},
			},
			expectedNumberOfTransactions: 0,
		},
		{
			name: "end of sprint - no bridge batch",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: false,
				IsEndOfSprint:       true,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 45},
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) {
				bridgeManagerMock := NewBridgeManagerMock(t)
				bridgeManagerMock.On("BridgeBatch", blockInfo.CurrentBlock(), mock.Anything).Return(nil, nil)

				bridge.bridgeManagers = map[uint64]BridgeManager{
					1: bridgeManagerMock,
				}
			},
			expectedNumberOfTransactions: 0,
		},
		{
			name: "end of sprint - has bridge batch",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfEpoch:        false,
				IsFirstBlockOfEpoch: false,
				IsEndOfSprint:       true,
				EpochSize:           10,
				ParentBlock:         &types.Header{Number: 45},
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) {
				signature, _ := createValidatorsAndSignHash(t, 5, types.StringToHash("0x123"))

				bridgeManagerMock := NewBridgeManagerMock(t)
				bridgeManagerMock.On("BridgeBatch", blockInfo.CurrentBlock(), mock.Anything).Return(&BridgeBatchSigned{
					BridgeBatch: &contractsapi.BridgeBatch{
						RootHash:           types.StringToHash("0x123"),
						StartID:            big.NewInt(1),
						EndID:              big.NewInt(10),
						SourceChainID:      big.NewInt(1),
						DestinationChainID: big.NewInt(2),
					},
					AggSignature: *signature,
				}, nil)

				bridge.bridgeManagers = map[uint64]BridgeManager{
					1: bridgeManagerMock,
				}
			},
			expectedNumberOfTransactions: 2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bridge := &bridge{}

			if tt.setupMocks != nil {
				tt.setupMocks(bridge, tt.blockInfo)
			}

			transactions, err := bridge.GetTransactions(*tt.blockInfo)
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			require.Len(t, transactions, tt.expectedNumberOfTransactions)
		})
	}
}

func TestVerifyTransactions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		blockInfo     *oracle.NewBlockInfo
		expectedError string
		setupMocks    func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction
	}{
		{
			name: "no transactions",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfSprint: true,
				ParentBlock:   &types.Header{Number: 5},
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				extra := &polytypes.Extra{
					Validators:    nil,
					BlockMetaData: &polytypes.BlockMetaData{EpochNumber: 1},
				}

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)

				return nil
			},
		},
		{
			name: "transaction with invalid input",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfSprint: true,
				ParentBlock:   &types.Header{Number: 5},
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput([]byte{0, 1, 2}))),
				}
			},
			expectedError: helpers.ErrStateTransactionInputInvalid.Error(),
		},
		{
			name: "bridge txn in a block that is not end of sprint",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfSprint: false,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				batch, _, _ := createAndSignBridgeBatch(t, 5, 1, 1, 10, 2, 1)

				input, err := batch.EncodeAbi()
				require.NoError(t, err)

				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
				}
			},
			expectedError: errBridgeBatchTxInNonSprintBlock.Error(),
		},
		{
			name: "bridge txn in a block that is end of sprint",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfSprint:     true,
				ParentBlock:       &types.Header{Number: 4},
				CurrentEpoch:      1,
				FirstBlockInEpoch: 1,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				batch, _, validators := createAndSignBridgeBatch(t, 5, blockInfo.CurrentEpoch, 1, 10, 2, 1)

				input, err := batch.EncodeAbi()
				require.NoError(t, err)

				blockInfo.CurrentEpochValidatorSet = validators.ToValidatorSet()

				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
				}
			},
		},
		{
			name: "two bridge transactions",
			blockInfo: &oracle.NewBlockInfo{
				IsEndOfSprint:     true,
				ParentBlock:       &types.Header{Number: 14},
				CurrentEpoch:      2,
				FirstBlockInEpoch: 11,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				batch, _, validators := createAndSignBridgeBatch(t, 5, blockInfo.CurrentEpoch, 1, 10, 2, 1)

				input, err := batch.EncodeAbi()
				require.NoError(t, err)

				blockInfo.CurrentEpochValidatorSet = validators.ToValidatorSet()

				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
				}
			},
			expectedError: errBridgeBatchTxExists.Error(),
		},
		{
			name: "commit validator set not exist when expected",
			blockInfo: &oracle.NewBlockInfo{
				IsFirstBlockOfEpoch: true,
				ParentBlock:         &types.Header{Number: 20},
				CurrentEpoch:        3,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				extra, validators, _ := createAndSignExtra(t, 5, 1, blockInfo.CurrentEpoch)

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)
				blockInfo.CurrentEpochValidatorSet = validators.ToValidatorSet()

				return nil
			},
			expectedError: errCommitValidatorSetTxDoesNotExist.Error(),
		},
		{
			name: "commit validator set transaction valid",
			blockInfo: &oracle.NewBlockInfo{
				IsFirstBlockOfEpoch: true,
				ParentBlock:         &types.Header{Number: 10},
				CurrentEpoch:        2,
				EpochSize:           10,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				extra, validators, commitFn := createAndSignExtra(t, 5, 1, blockInfo.CurrentEpoch)

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)
				blockInfo.CurrentEpochValidatorSet = validators.ToValidatorSet()

				input, err := commitFn.EncodeAbi()
				require.NoError(t, err)

				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
				}
			},
		},
		{
			name: "commit validator set transaction not expected",
			blockInfo: &oracle.NewBlockInfo{
				IsFirstBlockOfEpoch: false,
				ParentBlock:         &types.Header{Number: 11},
				CurrentEpoch:        2,
				EpochSize:           10,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				extra, validators, commitFn := createAndSignExtra(t, 5, 1, blockInfo.CurrentEpoch)

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)
				blockInfo.CurrentEpochValidatorSet = validators.ToValidatorSet()

				input, err := commitFn.EncodeAbi()
				require.NoError(t, err)

				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
				}
			},
			expectedError: errCommitValidatorSetTxNotExpected.Error(),
		},
		{
			name: "commit validator set transaction invalid - no validator set change",
			blockInfo: &oracle.NewBlockInfo{
				IsFirstBlockOfEpoch: true,
				ParentBlock:         &types.Header{Number: 21},
				CurrentEpoch:        3,
				EpochSize:           10,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				extra, validators, commitFn := createAndSignExtra(t, 3, 0, blockInfo.CurrentEpoch)

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)
				blockInfo.CurrentEpochValidatorSet = validators.ToValidatorSet()

				input, err := commitFn.EncodeAbi()
				require.NoError(t, err)

				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
				}
			},
			expectedError: errCommitValidatorSetTxInvalid.Error(),
		},
		{
			name: "commit validator set transaction invalid - validator set missmatch",
			blockInfo: &oracle.NewBlockInfo{
				IsFirstBlockOfEpoch: true,
				ParentBlock:         &types.Header{Number: 31},
				CurrentEpoch:        4,
				EpochSize:           10,
			},
			setupMocks: func(bridge *bridge, blockInfo *oracle.NewBlockInfo) []*types.Transaction {
				extra, _, commitFn := createAndSignExtra(t, 7, 1, blockInfo.CurrentEpoch)

				blockInfo.ParentBlock.ExtraData = extra.MarshalRLPTo(nil)
				blockInfo.CurrentEpochValidatorSet = validator.NewTestValidatorsWithAliases(t,
					[]string{"A", "B", "C"}).ToValidatorSet() // different validators

				input, err := commitFn.EncodeAbi()
				require.NoError(t, err)

				return []*types.Transaction{
					types.NewTx(types.NewStateTx(types.WithInput(input), types.WithTo(&contracts.BridgeStorageContract))),
				}
			},
			expectedError: "missing in the commit validator set transaction",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bridge := &bridge{}

			var txs []*types.Transaction
			if tt.setupMocks != nil {
				txs = tt.setupMocks(bridge, tt.blockInfo)
			}

			err := bridge.VerifyTransactions(*tt.blockInfo, txs)
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createAndSignExtra(t *testing.T, numOfValidators, numOfNewValidators int,
	epoch uint64) (*polytypes.Extra, *validator.TestValidators, *contractsapi.CommitValidatorSetBridgeStorageFn) {
	t.Helper()

	proposalHash := types.BytesToHash([]byte("test"))
	signature, validators := createValidatorsAndSignHash(t, numOfValidators+numOfValidators, proposalHash)

	var delta *validator.ValidatorSetDelta
	if numOfNewValidators > 0 {
		delta = &validator.ValidatorSetDelta{
			Added:   validators.GetPublicIdentities()[numOfValidators:],
			Removed: bitmap.Bitmap{},
		}
	}

	extra := &polytypes.Extra{
		Validators:    delta,
		Committed:     signature,
		BlockMetaData: &polytypes.BlockMetaData{EpochNumber: epoch},
	}

	sig, err := bls.UnmarshalSignature(signature.AggregatedSignature)
	require.NoError(t, err)

	sigBig, err := sig.ToBigInt()
	require.NoError(t, err)

	return extra, validators, &contractsapi.CommitValidatorSetBridgeStorageFn{
		NewValidatorSet: validators.ToValidatorSet().Accounts().ToABIBinding(),
		Signature:       sigBig,
		Bitmap:          signature.Bitmap,
	}
}

func createAndSignBridgeBatch(t *testing.T, numOfValidators int,
	epoch, startID, endID, destinationChainID, sourceChainID uint64,
) (*BridgeBatchSigned, *polytypes.Signature, *validator.TestValidators) {
	t.Helper()

	pendingBridgeBatch := &PendingBridgeBatch{
		BridgeBatch: &contractsapi.BridgeBatch{
			RootHash:           types.StringToHash("0x123"),
			StartID:            big.NewInt(int64(startID)),
			EndID:              big.NewInt(int64(endID)),
			SourceChainID:      big.NewInt(int64(sourceChainID)),
			DestinationChainID: big.NewInt(int64(destinationChainID)),
		},
		Epoch: epoch,
	}

	hash, err := pendingBridgeBatch.Hash()
	require.NoError(t, err)

	signature, validators := createValidatorsAndSignHash(t, numOfValidators, hash)

	return &BridgeBatchSigned{
		BridgeBatch:  pendingBridgeBatch.BridgeBatch,
		AggSignature: *signature,
	}, signature, validators
}

func createValidatorsAndSignHash(t *testing.T, numOfValidators int,
	hashToSign types.Hash) (*polytypes.Signature, *validator.TestValidators) {
	t.Helper()

	validators := validator.NewTestValidators(t, numOfValidators)
	signatures := make(bls.Signatures, 0, numOfValidators)
	bmap := bitmap.Bitmap{}

	for i, v := range validators.GetValidators() {
		sig, err := v.Key().SignWithDomain(hashToSign.Bytes(), signer.DomainBridge)
		require.NoError(t, err)

		signature, err := bls.UnmarshalSignature(sig)
		require.NoError(t, err)

		signatures = append(signatures, signature)

		bmap.Set(uint64(i))

		if i == numOfValidators-1 {
			break
		}
	}

	aggregatedSignature, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	return &polytypes.Signature{Bitmap: bmap, AggregatedSignature: aggregatedSignature}, validators
}
