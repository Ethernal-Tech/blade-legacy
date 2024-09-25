package stake

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

// newTestState creates new instance of state used by tests.
func newTestState(tb testing.TB) *StakeStore {
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

	stakeSTore, err := newStakeStore(db)
	if err != nil {
		tb.Fatal(err)
	}

	return stakeSTore
}

func TestState_Insert_And_Get_FullValidatorSet(t *testing.T) {
	state := newTestState(t)

	t.Run("No full validator set", func(t *testing.T) {
		_, err := state.getFullValidatorSet(nil)

		require.ErrorIs(t, err, errNoFullValidatorSet)
	})

	t.Run("Insert validator set", func(t *testing.T) {
		validators := validator.NewTestValidators(t, 5).GetPublicIdentities()

		assert.NoError(t, state.insertFullValidatorSet(validator.ValidatorSetState{
			BlockNumber: 100,
			EpochID:     10,
			Validators:  validator.NewValidatorStakeMap(validators),
		}, nil))

		fullValidatorSet, err := state.getFullValidatorSet(nil)
		require.NoError(t, err)
		assert.Equal(t, uint64(100), fullValidatorSet.BlockNumber)
		assert.Equal(t, uint64(10), fullValidatorSet.EpochID)
		assert.Len(t, fullValidatorSet.Validators, len(validators))
	})

	t.Run("Update validator set", func(t *testing.T) {
		validators := validator.NewTestValidators(t, 10).GetPublicIdentities()

		assert.NoError(t, state.insertFullValidatorSet(validator.ValidatorSetState{
			BlockNumber: 40,
			EpochID:     4,
			Validators:  validator.NewValidatorStakeMap(validators),
		}, nil))

		fullValidatorSet, err := state.getFullValidatorSet(nil)
		require.NoError(t, err)
		assert.Len(t, fullValidatorSet.Validators, len(validators))
		assert.Equal(t, uint64(40), fullValidatorSet.BlockNumber)
		assert.Equal(t, uint64(4), fullValidatorSet.EpochID)
	})
}
