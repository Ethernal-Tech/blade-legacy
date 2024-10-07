package contractsapi

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/abi"
	"github.com/stretchr/testify/require"
)

func TestEncoding_Method(t *testing.T) {
	t.Parallel()

	cases := []ABIEncoder{
		// empty commit epoch
		&CommitEpochEpochManagerFn{
			ID: big.NewInt(1),
			Epoch: &Epoch{
				StartBlock: big.NewInt(1),
				EndBlock:   big.NewInt(1),
			},
			EpochSize: big.NewInt(10),
		},
	}

	for _, c := range cases {
		res, err := c.EncodeAbi()
		require.NoError(t, err)

		// use reflection to create another type and decode
		val := reflect.New(reflect.TypeOf(c).Elem()).Interface()
		obj, ok := val.(ABIEncoder)
		require.True(t, ok)

		err = obj.DecodeAbi(res)
		require.NoError(t, err)
		require.Equal(t, obj, c)
	}
}

func TestEncoding_Struct(t *testing.T) {
	t.Parallel()

	bridgeBatch := BridgeMessageBatch{
		Messages:           []*BridgeMessage{},
		SourceChainID:      big.NewInt(1),
		DestinationChainID: big.NewInt(0),
	}

	encoding, err := bridgeBatch.EncodeAbi()
	require.NoError(t, err)

	var bridgeBatchDecoded BridgeMessageBatch

	require.NoError(t, bridgeBatchDecoded.DecodeAbi(encoding))
	require.Equal(t, bridgeBatch.SourceChainID, bridgeBatchDecoded.SourceChainID)
	require.Equal(t, bridgeBatch.DestinationChainID.Uint64(), bridgeBatchDecoded.DestinationChainID.Uint64())
}

func TestEncodingAndParsingEvent(t *testing.T) {
	t.Parallel()

	var (
		bridgeMsgEvent BridgeMsgEvent
	)

	topics := make([]ethgo.Hash, 4)
	topics[0] = bridgeMsgEvent.Sig()
	topics[1] = ethgo.BytesToHash(common.EncodeUint64ToBytes(11))
	topics[2] = ethgo.BytesToHash(types.StringToAddress("0x1111").Bytes())
	topics[3] = ethgo.BytesToHash(types.StringToAddress("0x2222").Bytes())
	someType := abi.MustNewType("tuple(string firstName,string secondName ,string lastName)")
	encodedData, err := someType.Encode(map[string]string{"firstName": "John", "secondName": "data", "lastName": "Doe"})
	require.NoError(t, err)

	log := &ethgo.Log{
		Topics: topics,
		Data:   encodedData,
	}

	// log matches event
	doesMatch, err := bridgeMsgEvent.ParseLog(log)
	require.NoError(t, err)
	require.True(t, doesMatch)
	require.Equal(t, uint64(11), bridgeMsgEvent.ID.Uint64())

	// change exit event id
	log.Topics[1] = ethgo.BytesToHash(common.EncodeUint64ToBytes(22))
	doesMatch, err = bridgeMsgEvent.ParseLog(log)
	require.NoError(t, err)
	require.True(t, doesMatch)
	require.Equal(t, uint64(22), bridgeMsgEvent.ID.Uint64())

	// error on parsing log
	log.Topics[0] = bridgeMsgEvent.Sig()
	log.Topics = log.Topics[:3]
	doesMatch, err = bridgeMsgEvent.ParseLog(log)
	require.Error(t, err)
	require.True(t, doesMatch)
}
