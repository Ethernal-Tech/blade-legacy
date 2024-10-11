package contractsapi

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

func TestEncoding_SignedTypes(t *testing.T) {
	t.Parallel()

	cases := []ABIEncoder{
		&SignedValidatorSet{
			NewValidatorSet: []*Validator{},
			Signature:       [2]*big.Int{big.NewInt(1), big.NewInt(2)},
			Bitmap:          big.NewInt(1).Bytes(),
		},
		&SignedBridgeMessageBatch{
			RootHash:           types.StringToHash("0x1555ad6149fc39abc7852aad5c3df6b9df7964ac90ffbbcf6206b1eda846c881"),
			StartID:            big.NewInt(1),
			EndID:              big.NewInt(5),
			SourceChainID:      big.NewInt(2),
			DestinationChainID: big.NewInt(3),
			Signature:          [2]*big.Int{big.NewInt(300), big.NewInt(200)},
			Bitmap:             []byte("smth"),
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
