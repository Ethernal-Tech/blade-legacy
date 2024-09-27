package validatorsnapshot

import (
	"encoding/json"
	"fmt"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	"github.com/0xPolygon/polygon-edge/helper/common"
	bolt "go.etcd.io/bbolt"
)

var (
	// bucket to store validator snapshots
	validatorSnapshotsBucket = []byte("validatorSnapshots")
)

type validatorSnapshotStore struct {
	db *bolt.DB
}

func newValidatorSnapshotStore(db *bolt.DB) (*validatorSnapshotStore, error) {
	store := &validatorSnapshotStore{db: db}

	return store, store.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(validatorSnapshotsBucket); err != nil {
			return fmt.Errorf("failed to create bucket=%s: %w", string(validatorSnapshotsBucket), err)
		}

		return nil
	})
}

// insertValidatorSnapshot inserts a validator snapshot for the given block to its bucket in db
func (s *validatorSnapshotStore) insertValidatorSnapshot(validatorSnapshot *ValidatorSnapshot, dbTx *bolt.Tx) error {
	insertFn := func(tx *bolt.Tx) error {
		raw, err := json.Marshal(validatorSnapshot)
		if err != nil {
			return err
		}

		return tx.Bucket(validatorSnapshotsBucket).Put(common.EncodeUint64ToBytes(validatorSnapshot.Epoch), raw)
	}

	if dbTx == nil {
		return s.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	}

	return insertFn(dbTx)
}

// getValidatorSnapshot queries the validator snapshot for given block from db
func (s *validatorSnapshotStore) getValidatorSnapshot(epoch uint64) (*ValidatorSnapshot, error) {
	var validatorSnapshot *ValidatorSnapshot

	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(validatorSnapshotsBucket).Get(common.EncodeUint64ToBytes(epoch))
		if v != nil {
			return json.Unmarshal(v, &validatorSnapshot)
		}

		return nil
	})

	return validatorSnapshot, err
}

// getNearestOrEpochSnapshot returns the nearest or the exact epoch snapshot from db
func (s *validatorSnapshotStore) getNearestOrEpochSnapshot(epoch uint64, dbTx *bolt.Tx) (*ValidatorSnapshot, error) {
	var (
		snapshot *ValidatorSnapshot
		err      error
	)

	getFn := func(tx *bolt.Tx) error {
		for {
			v := tx.Bucket(validatorSnapshotsBucket).Get(common.EncodeUint64ToBytes(epoch))
			if v != nil {
				return json.Unmarshal(v, &snapshot)
			}

			if epoch == 0 { // prevent uint64 underflow
				break
			}

			epoch--
		}

		return nil
	}

	if dbTx == nil {
		err = s.db.View(func(tx *bolt.Tx) error {
			return getFn(tx)
		})
	} else {
		err = getFn(dbTx)
	}

	return snapshot, err
}

// cleanValidatorSnapshotsFromDB cleans the validator snapshots bucket if a limit is reached,
// but it leaves the latest (n) number of snapshots
func (s *validatorSnapshotStore) cleanValidatorSnapshotsFromDB(epoch uint64, dbTx *bolt.Tx) error {
	cleanFn := func(tx *bolt.Tx) error {
		bucket := tx.Bucket(validatorSnapshotsBucket)

		// paired list
		keys := make([][]byte, 0)
		values := make([][]byte, 0)

		for i := 0; i < NumberOfSnapshotsToLeaveInDB; i++ { // exclude the last inserted we already appended
			key := common.EncodeUint64ToBytes(epoch)
			value := bucket.Get(key)

			if value == nil {
				continue
			}

			keys = append(keys, key)
			values = append(values, value)
			epoch--
		}

		// removing an entire bucket is much faster than removing all keys
		// look at thread https://github.com/boltdb/bolt/issues/667
		err := tx.DeleteBucket(validatorSnapshotsBucket)
		if err != nil {
			return err
		}

		bucket, err = tx.CreateBucket(validatorSnapshotsBucket)
		if err != nil {
			return err
		}

		// we start the loop in reverse so that the oldest of snapshots get inserted first in db
		for i := len(keys) - 1; i >= 0; i-- {
			if err := bucket.Put(keys[i], values[i]); err != nil {
				return err
			}
		}

		return nil
	}

	if dbTx == nil {
		return s.db.Update(func(tx *bolt.Tx) error {
			return cleanFn(tx)
		})
	}

	return cleanFn(dbTx)
}

// validatorSnapshotsDBStats returns stats of validators snapshot bucket in db
func (s *validatorSnapshotStore) validatorSnapshotsDBStats() (*bolt.BucketStats, error) {
	return state.BucketStats(validatorSnapshotsBucket, s.db)
}

// beginDBTransaction creates and begins a transaction on BoltDB
// Note that transaction needs to be manually rollback or committed
func (s *validatorSnapshotStore) beginDBTransaction(isWriteTx bool) (*bolt.Tx, error) {
	return s.db.Begin(isWriteTx)
}

// removeAllValidatorSnapshots drops a validator snapshot bucket and re-creates it in bolt database
func (s *validatorSnapshotStore) removeAllValidatorSnapshots() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		// removing an entire bucket is much faster than removing all keys
		// look at thread https://github.com/boltdb/bolt/issues/667
		err := tx.DeleteBucket(validatorSnapshotsBucket)
		if err != nil {
			return err
		}

		_, err = tx.CreateBucket(validatorSnapshotsBucket)
		if err != nil {
			return err
		}

		return nil
	})
}
