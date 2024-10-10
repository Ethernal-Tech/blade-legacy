package bridge

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	polytesting "github.com/0xPolygon/polygon-edge/consensus/polybft/testing"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

var TestDomain = crypto.Keccak256([]byte("DOMAIN"))

func BuildBridgeBatchAndBridgeEvents(t *testing.T, bridgeMessageCount int,
	epoch, startIdx uint64) (*PendingBridgeBatch, *BridgeBatchSigned, []*contractsapi.BridgeMsgEvent) {
	t.Helper()

	bridgeMessageEvents := generateBridgeMessageEvents(t, bridgeMessageCount, startIdx)
	pendingBridgeBatch, err := NewPendingBridgeBatch(epoch, bridgeMessageEvents)
	require.NoError(t, err)

	blsKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data, err := pendingBridgeBatch.BridgeBatch.EncodeAbi()
	require.NoError(t, err)

	signature, err := blsKey.Sign(data, TestDomain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	bridgeBatchSigned := &BridgeBatchSigned{
		BridgeBatch: pendingBridgeBatch.BridgeBatch,
		AggSignature: polytypes.Signature{
			AggregatedSignature: aggSig,
			Bitmap:              []byte{},
		},
	}

	return pendingBridgeBatch, bridgeBatchSigned, bridgeMessageEvents
}

func generateBridgeMessageEvents(t *testing.T, eventsCount int, startIdx uint64) []*contractsapi.BridgeMsgEvent {
	t.Helper()

	bridgeMessageEvents := make([]*contractsapi.BridgeMsgEvent, eventsCount)
	for i := 0; i < eventsCount; i++ {
		bridgeMessageEvents[i] = &contractsapi.BridgeMsgEvent{
			ID:                 big.NewInt(int64(startIdx + uint64(i))), //nolint:gosec
			Sender:             types.StringToAddress(fmt.Sprintf("0x5%d", i)),
			Receiver:           types.StringToAddress(fmt.Sprintf("0x4%d", i)),
			Data:               polytesting.GenerateRandomBytes(t),
			SourceChainID:      big.NewInt(1),
			DestinationChainID: big.NewInt(0),
		}
	}

	return bridgeMessageEvents
}

func CreateTestBridgeBatchMessage(t *testing.T, numberOfMessages, firstIndex uint64) *BridgeBatchSigned {
	t.Helper()

	msg := contractsapi.BridgeBatch{
		StartID:            big.NewInt(1),
		EndID:              big.NewInt(int64(numberOfMessages)),
		SourceChainID:      big.NewInt(1),
		DestinationChainID: big.NewInt(0),
	}

	blsKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data := polytesting.GenerateRandomBytes(t)

	signature, err := blsKey.Sign(data, TestDomain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	return &BridgeBatchSigned{
		BridgeBatch:  &msg,
		AggSignature: polytypes.Signature{AggregatedSignature: aggSig},
	}
}
