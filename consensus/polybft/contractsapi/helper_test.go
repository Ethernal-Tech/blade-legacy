package contractsapi

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type signedType interface {
	EncodeAbi() ([]byte, error)
	DecodeAbi(buf []byte) error
}

func TestEncoding_SignedTypes(t *testing.T) {
	t.Parallel()

	cases := []signedType{
		&SignedValidatorSet{
			NewValidatorSet: []*Validator{},
			Signature:       [2]*big.Int{big.NewInt(1), big.NewInt(2)},
			Bitmap:          big.NewInt(1).Bytes(),
		},
		&SignedBridgeMessageBatch{
			Batch:     &BridgeMessageBatch{Messages: []*BridgeMessage{}, SourceChainID: big.NewInt(1), DestinationChainID: big.NewInt(2)},
			Signature: [2]*big.Int{big.NewInt(1), big.NewInt(2)},
			Bitmap:    big.NewInt(1).Bytes(),
		},
	}

	for _, c := range cases {
		res, err := c.EncodeAbi()
		require.NoError(t, err)

		// use reflection to create another type and decode
		val := reflect.New(reflect.TypeOf(c).Elem()).Interface()
		obj, ok := val.(signedType)
		require.True(t, ok)

		err = obj.DecodeAbi(res)
		require.NoError(t, err)
		require.Equal(t, obj, c)
	}
}
