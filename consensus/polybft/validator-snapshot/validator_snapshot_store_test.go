package validatorsnapshot

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

// newTestState creates new instance of state used by tests.
func newTestState(tb testing.TB) *validatorSnapshotStore {
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

	validatorSnapshotStore, err := newValidatorSnapshotStore(db)
	if err != nil {
		tb.Fatal(err)
	}

	return validatorSnapshotStore
}

func TestState_insertAndGetValidatorSnapshot(t *testing.T) {
	t.Parallel()

	const (
		epoch            = uint64(1)
		epochEndingBlock = uint64(100)
	)

	state := newTestState(t)
	keys, err := bls.CreateRandomBlsKeys(3)

	require.NoError(t, err)

	snapshot := validator.AccountSet{
		&validator.ValidatorMetadata{Address: types.BytesToAddress([]byte{0x18}), BlsKey: keys[0].PublicKey()},
		&validator.ValidatorMetadata{Address: types.BytesToAddress([]byte{0x23}), BlsKey: keys[1].PublicKey()},
		&validator.ValidatorMetadata{Address: types.BytesToAddress([]byte{0x37}), BlsKey: keys[2].PublicKey()},
	}

	assert.NoError(t, state.insertValidatorSnapshot(
		&ValidatorSnapshot{
			Epoch:            epoch,
			EpochEndingBlock: epochEndingBlock,
			Snapshot:         snapshot,
		}, nil))

	snapshotFromDB, err := state.getValidatorSnapshot(epoch)

	assert.NoError(t, err)
	assert.Equal(t, snapshot.Len(), snapshotFromDB.Snapshot.Len())
	assert.Equal(t, epoch, snapshotFromDB.Epoch)
	assert.Equal(t, epochEndingBlock, snapshotFromDB.EpochEndingBlock)

	for i, v := range snapshot {
		assert.Equal(t, v.Address, snapshotFromDB.Snapshot[i].Address)
		assert.Equal(t, v.BlsKey, snapshotFromDB.Snapshot[i].BlsKey)
	}
}

func TestState_cleanValidatorSnapshotsFromDb(t *testing.T) {
	t.Parallel()

	fixedEpochSize := uint64(10)
	state := newTestState(t)
	keys, err := bls.CreateRandomBlsKeys(3)
	require.NoError(t, err)

	snapshot := validator.AccountSet{
		&validator.ValidatorMetadata{Address: types.BytesToAddress([]byte{0x18}), BlsKey: keys[0].PublicKey()},
		&validator.ValidatorMetadata{Address: types.BytesToAddress([]byte{0x23}), BlsKey: keys[1].PublicKey()},
		&validator.ValidatorMetadata{Address: types.BytesToAddress([]byte{0x37}), BlsKey: keys[2].PublicKey()},
	}

	var epoch uint64
	// add a couple of more snapshots above limit just to make sure we reached it
	for i := 1; i <= ValidatorSnapshotLimit+2; i++ {
		epoch = uint64(i)
		assert.NoError(t, state.insertValidatorSnapshot(
			&ValidatorSnapshot{
				Epoch:            epoch,
				EpochEndingBlock: epoch * fixedEpochSize,
				Snapshot:         snapshot,
			}, nil))
	}

	snapshotFromDB, err := state.getValidatorSnapshot(epoch)

	assert.NoError(t, err)
	assert.Equal(t, snapshot.Len(), snapshotFromDB.Snapshot.Len())
	assert.Equal(t, epoch, snapshotFromDB.Epoch)
	assert.Equal(t, epoch*fixedEpochSize, snapshotFromDB.EpochEndingBlock)

	for i, v := range snapshot {
		assert.Equal(t, v.Address, snapshotFromDB.Snapshot[i].Address)
		assert.Equal(t, v.BlsKey, snapshotFromDB.Snapshot[i].BlsKey)
	}

	assert.NoError(t, state.cleanValidatorSnapshotsFromDB(epoch, nil))

	// test that last (numberOfSnapshotsToLeaveInDb) of snapshots are left in db after cleanup
	validatorSnapshotsBucketStats, err := state.validatorSnapshotsDBStats()
	require.NoError(t, err)

	assert.Equal(t, NumberOfSnapshotsToLeaveInDB, validatorSnapshotsBucketStats.KeyN)

	for i := 0; i < NumberOfSnapshotsToLeaveInDB; i++ {
		snapshotFromDB, err = state.getValidatorSnapshot(epoch)
		assert.NoError(t, err)
		assert.NotNil(t, snapshotFromDB)

		epoch--
	}
}

func TestEpochStore_getNearestOrEpochSnapshot(t *testing.T) {
	t.Parallel()

	state := newTestState(t)
	epoch := uint64(1)
	tv := validator.NewTestValidators(t, 3)

	// Insert a snapshot for epoch 1
	snapshot := &ValidatorSnapshot{
		Epoch:            epoch,
		EpochEndingBlock: 100,
		Snapshot:         tv.GetPublicIdentities(),
	}

	require.NoError(t, state.insertValidatorSnapshot(snapshot, nil))

	t.Run("with existing dbTx", func(t *testing.T) {
		t.Parallel()

		dbTx, err := state.beginDBTransaction(false)
		require.NoError(t, err)

		result, err := state.getNearestOrEpochSnapshot(epoch, dbTx)
		assert.NoError(t, err)
		assert.Equal(t, snapshot, result)

		require.NoError(t, dbTx.Rollback())
	})

	t.Run("without existing dbTx", func(t *testing.T) {
		t.Parallel()

		result, err := state.getNearestOrEpochSnapshot(epoch, nil)
		assert.NoError(t, err)
		assert.Equal(t, snapshot, result)
	})

	t.Run("with non-existing epoch", func(t *testing.T) {
		t.Parallel()

		result, err := state.getNearestOrEpochSnapshot(2, nil)
		assert.NoError(t, err)
		assert.Equal(t, snapshot, result)
	})
}
