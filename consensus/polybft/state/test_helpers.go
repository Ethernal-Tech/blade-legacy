package state

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"
)

// newTestState creates new instance of state used by tests.
func NewTestState(tb testing.TB) *State {
	tb.Helper()

	dir := fmt.Sprintf("/tmp/consensus-temp_%v", time.Now().UTC().Format(time.RFC3339Nano))
	err := os.Mkdir(dir, 0775)

	if err != nil {
		tb.Fatal(err)
	}

	state, err := NewState(path.Join(dir, "my.db"), make(chan struct{}))
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			tb.Fatal(err)
		}
	})

	return state
}
