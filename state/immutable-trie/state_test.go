package itrie

import (
	"testing"

	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
)

func TestState(t *testing.T) {
	state.TestState(t, buildPreState)
}

func buildPreState(pre state.PreStates) (state.Snapshot, error) {
	storage := NewMemoryStorage()
	st := NewState(storage)

	return st.NewSnapshot(types.ZeroHash)
}
