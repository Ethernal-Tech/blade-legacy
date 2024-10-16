package types

import (
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	polytesting "github.com/0xPolygon/polygon-edge/consensus/polybft/testing"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
)

func CreateTestBlocks(t *testing.T, numberOfBlocks, defaultEpochSize uint64,
	validatorSet validator.AccountSet) (*types.Header, *polytesting.TestHeadersMap) {
	t.Helper()

	headerMap := &polytesting.TestHeadersMap{}
	bitmaps := createTestBitmaps(t, validatorSet, numberOfBlocks)

	extra := &Extra{
		BlockMetaData: &BlockMetaData{EpochNumber: 0},
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
			ExtraData:  CreateTestExtraForAccounts(t, getEpochNumber(t, i, defaultEpochSize), validatorSet, bitmaps[i]),
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

func CreateTestExtraForAccounts(t *testing.T, epoch uint64, validators validator.AccountSet, b bitmap.Bitmap) []byte {
	t.Helper()

	dummySignature := [64]byte{}
	extraData := Extra{
		Validators: &validator.ValidatorSetDelta{
			Added:   validators,
			Removed: bitmap.Bitmap{},
		},
		Parent:        &Signature{Bitmap: b, AggregatedSignature: dummySignature[:]},
		Committed:     &Signature{Bitmap: b, AggregatedSignature: dummySignature[:]},
		BlockMetaData: &BlockMetaData{EpochNumber: epoch},
	}

	return extraData.MarshalRLPTo(nil)
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
