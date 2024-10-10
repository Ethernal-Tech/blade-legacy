package bridge

import (
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/stretchr/testify/require"
)

func TestBridgeBatchSigned_Hash(t *testing.T) {
	t.Parallel()

	bridgeBatchSigned1 := newTestBridgeBatchSigned(t, 1, 0)
	bridgeBatchSigned2 := newTestBridgeBatchSigned(t, 1, 0)
	bridgeBatchSigned3 := newTestBridgeBatchSigned(t, 2, 0)
	bridgeBatchSigned4 := newTestBridgeBatchSigned(t, 1, 3)

	hash1, err := bridgeBatchSigned1.Hash()
	require.NoError(t, err)
	hash2, err := bridgeBatchSigned2.Hash()
	require.NoError(t, err)
	hash3, err := bridgeBatchSigned3.Hash()
	require.NoError(t, err)
	hash4, err := bridgeBatchSigned4.Hash()
	require.NoError(t, err)

	require.Equal(t, hash1, hash2)
	require.NotEqual(t, hash1, hash3)
	require.NotEqual(t, hash1, hash4)
	require.NotEqual(t, hash3, hash4)
}

func TestBridgeBatch_BridgeBatchEncodeDecode(t *testing.T) {
	t.Parallel()

	const epoch, eventsCount = uint64(100), 11
	pendingBridgeBatch, _, _ := BuildBridgeBatchAndBridgeEvents(t, eventsCount, epoch, uint64(2))
	blsKey1, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	blsKey2, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data, err := pendingBridgeBatch.BridgeBatch.EncodeAbi()
	require.NoError(t, err)

	signature1, err := blsKey1.Sign(data, TestDomain)
	require.NoError(t, err)

	signature2, err := blsKey2.Sign(data, TestDomain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature1, signature2}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	expectedSignedBridgeBatchMsg := &BridgeBatchSigned{
		BridgeBatch: pendingBridgeBatch.BridgeBatch,
		AggSignature: polytypes.Signature{
			Bitmap:              []byte{5, 1},
			AggregatedSignature: aggSig,
		},
	}
	inputData, err := expectedSignedBridgeBatchMsg.EncodeAbi()
	require.NoError(t, err)
	require.NotEmpty(t, inputData)

	var actualSignedBridgeBatchMsg BridgeBatchSigned

	require.NoError(t, actualSignedBridgeBatchMsg.DecodeAbi(inputData))
	require.Equal(t, expectedSignedBridgeBatchMsg.BridgeBatch.StartID.Uint64(), actualSignedBridgeBatchMsg.BridgeBatch.EndID.Uint64())
	require.Equal(t, expectedSignedBridgeBatchMsg.BridgeBatch.EndID.Uint64(), actualSignedBridgeBatchMsg.BridgeBatch.EndID.Uint64())
	require.Equal(t, expectedSignedBridgeBatchMsg.AggSignature, actualSignedBridgeBatchMsg.AggSignature)
}

func newTestBridgeBatchSigned(t *testing.T, sourceChainID, destinationChainID uint64) *BridgeBatchSigned {
	t.Helper()

	return &BridgeBatchSigned{
		BridgeBatch: &contractsapi.BridgeBatch{
			SourceChainID:      new(big.Int).SetUint64(sourceChainID),
			DestinationChainID: new(big.Int).SetUint64(destinationChainID),
		},
		AggSignature: polytypes.Signature{},
	}
}
