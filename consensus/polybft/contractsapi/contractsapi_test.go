package contractsapi

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/abi"
	"github.com/stretchr/testify/require"
)

type method interface {
	EncodeAbi() ([]byte, error)
	DecodeAbi(buf []byte) error
}

func TestEncoding_Method(t *testing.T) {
	t.Parallel()

	cases := []method{
		// empty commit
		&CommitStateReceiverFn{
			Commitment: &StateSyncCommitment{
				StartID: big.NewInt(1),
				EndID:   big.NewInt(1),
				Root:    types.EmptyRootHash,
			},
			Signature: []byte{},
			Bitmap:    []byte{},
		},
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
		obj, ok := val.(method)
		require.True(t, ok)

		err = obj.DecodeAbi(res)
		require.NoError(t, err)
		require.Equal(t, obj, c)
	}
}

func TestEncoding_Struct(t *testing.T) {
	t.Parallel()

	commitment := &StateSyncCommitment{
		StartID: big.NewInt(1),
		EndID:   big.NewInt(10),
		Root:    types.StringToHash("hash"),
	}

	encoding, err := commitment.EncodeAbi()
	require.NoError(t, err)

	var commitmentDecoded StateSyncCommitment

	require.NoError(t, commitmentDecoded.DecodeAbi(encoding))
	require.Equal(t, commitment.StartID, commitmentDecoded.StartID)
	require.Equal(t, commitment.EndID, commitmentDecoded.EndID)
	require.Equal(t, commitment.Root, commitmentDecoded.Root)
}

func TestEncodingAndParsingEvent(t *testing.T) {
	t.Parallel()

	var (
		bridgeMsgEventAPI BridgeMsgEvent
	)

	topics := make([]ethgo.Hash, 4)
	topics[0] = bridgeMsgEventAPI.Sig()
	topics[1] = ethgo.BytesToHash(common.EncodeUint64ToBytes(11))
	topics[2] = ethgo.BytesToHash(types.StringToAddress("0x1111").Bytes())
	topics[3] = ethgo.BytesToHash(types.StringToAddress("0x2222").Bytes())
	someType := abi.MustNewType("tuple(string firstName,string secondName ,string lastName)")
	encodedData, err := someType.Encode(map[string]string{"firstName": "John", "secondName": "data", "lastName": "Doe"})
	require.NoError(t, err)

	log := &ethgo.Log{
		Address: ethgo.Address(contracts.L2StateSenderContract),
		Topics:  topics,
		Data:    encodedData,
	}

	var exitEvent BridgeMsgEvent

	// log matches event
	doesMatch, err := exitEvent.ParseLog(log)
	require.NoError(t, err)
	require.True(t, doesMatch)
	require.Equal(t, uint64(11), exitEvent.ID.Uint64())

	// change exit event id
	log.Topics[1] = ethgo.BytesToHash(common.EncodeUint64ToBytes(22))
	doesMatch, err = exitEvent.ParseLog(log)
	require.NoError(t, err)
	require.True(t, doesMatch)
	require.Equal(t, uint64(22), exitEvent.ID.Uint64())

	// error on parsing log
	log.Topics[0] = bridgeMsgEventAPI.Sig()
	log.Topics = log.Topics[:3]
	doesMatch, err = exitEvent.ParseLog(log)
	require.Error(t, err)
	require.True(t, doesMatch)
}
