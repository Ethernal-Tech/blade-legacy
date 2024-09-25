package systemstate

import (
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/state"
	itrie "github.com/0xPolygon/polygon-edge/state/immutable-trie"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func NewTestTransition(t *testing.T, alloc map[types.Address]*chain.GenesisAccount) *state.Transition {
	t.Helper()

	st := itrie.NewState(itrie.NewMemoryStorage())

	ex := state.NewExecutor(&chain.Params{
		Forks: chain.AllForksEnabled,
		BurnContract: map[uint64]types.Address{
			0: types.ZeroAddress,
		},
	}, st, hclog.NewNullLogger())

	rootHash, err := ex.WriteGenesis(alloc, types.Hash{})
	require.NoError(t, err)

	ex.GetHash = func(h *types.Header) state.GetHashByNumber {
		return func(i uint64) types.Hash {
			return rootHash
		}
	}

	transition, err := ex.BeginTxn(
		rootHash,
		&types.Header{},
		types.ZeroAddress,
	)
	require.NoError(t, err)

	return transition
}
