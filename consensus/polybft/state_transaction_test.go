package polybft

import (
	"encoding/hex"
	"math/big"
	"reflect"
	"testing"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo/abi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateTransaction_Signature(t *testing.T) {
	t.Parallel()

	cases := []struct {
		m   *abi.Method
		sig string
	}{
		{
			contractsapi.EpochManager.Abi.GetMethod("commitEpoch"),
			"8db3a4c1",
		},
	}
	for _, c := range cases {
		sig := hex.EncodeToString(c.m.ID())
		require.Equal(t, c.sig, sig)
	}
}

func TestStateTransaction_Encoding(t *testing.T) {
	t.Parallel()

	cases := []contractsapi.StateTransactionInput{
		&contractsapi.CommitEpochEpochManagerFn{
			ID: big.NewInt(1),
			Epoch: &contractsapi.Epoch{
				StartBlock: big.NewInt(1),
				EndBlock:   big.NewInt(10),
				EpochRoot:  types.Hash{},
			},
			EpochSize: new(big.Int).SetUint64(10),
		},
	}

	for _, c := range cases {
		res, err := c.EncodeAbi()

		require.NoError(t, err)

		// use reflection to create another type and decode
		val := reflect.New(reflect.TypeOf(c).Elem()).Interface()
		obj, ok := val.(contractsapi.StateTransactionInput)
		assert.True(t, ok)

		err = obj.DecodeAbi(res)
		require.NoError(t, err)

		require.Equal(t, obj, c)
	}
}
