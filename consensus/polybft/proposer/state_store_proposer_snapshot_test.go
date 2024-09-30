package proposer

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

// newTestState creates new instance of state used by tests.
func newTestState(tb testing.TB) *ProposerSnapshotStore {
	tb.Helper()

	dir := fmt.Sprintf("/tmp/consensus-temp_%v", time.Now().UTC().Format(time.RFC3339Nano))
	err := os.Mkdir(dir, 0775)

	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			tb.Fatal(err)
		}
	})

	db, err := bolt.Open(path.Join(dir, "my.db"), 0666, nil)
	if err != nil {
		tb.Fatal(err)
	}

	proposerSnapshotStore, err := newProposerSnapshotStore(db, nil)
	if err != nil {
		tb.Fatal(err)
	}

	return proposerSnapshotStore
}

func TestState_getProposerSnapshot_writeProposerSnapshot(t *testing.T) {
	t.Parallel()

	const (
		height = uint64(100)
		round  = uint64(5)
	)

	state := newTestState(t)

	snap, err := state.getProposerSnapshot(nil)
	require.NoError(t, err)
	require.Nil(t, snap)

	newSnapshot := &ProposerSnapshot{Height: height, Round: round}
	require.NoError(t, state.writeProposerSnapshot(newSnapshot, nil))

	snap, err = state.getProposerSnapshot(nil)
	require.NoError(t, err)
	require.Equal(t, newSnapshot, snap)
}
