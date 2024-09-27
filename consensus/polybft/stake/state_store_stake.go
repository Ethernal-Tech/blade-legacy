package stake

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	bolt "go.etcd.io/bbolt"
)

var (
	// bucket to store full validator set
	validatorSetBucket = []byte("fullValidatorSetBucket")
	// key of the full validator set in bucket
	fullValidatorSetKey = []byte("fullValidatorSet")
)

type stakeStore struct {
	db *bolt.DB
}

func newStakeStore(db *bolt.DB, dbTx *bolt.Tx) (*stakeStore, error) {
	store := &stakeStore{db: db}

	initFn := func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(validatorSetBucket); err != nil {
			return fmt.Errorf("failed to create bucket=%s: %w", string(validatorSetBucket), err)
		}

		return nil
	}

	var err error

	if dbTx == nil {
		err = db.Update(initFn)
	} else {
		err = initFn(dbTx)
	}

	return store, err
}

// insertFullValidatorSet inserts full validator set to its bucket (or updates it if exists)
// If the passed tx is already open (not nil), it will use it to insert full validator set
// If the passed tx is not open (it is nil), it will open a new transaction on db and insert full validator set
func (s *stakeStore) insertFullValidatorSet(fullValidatorSet validator.ValidatorSetState, dbTx *bolt.Tx) error {
	insertFn := func(tx *bolt.Tx) error {
		raw, err := fullValidatorSet.Marshal()
		if err != nil {
			return err
		}

		return tx.Bucket(validatorSetBucket).Put(fullValidatorSetKey, raw)
	}

	if dbTx == nil {
		return s.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	}

	return insertFn(dbTx)
}

// getFullValidatorSet returns full validator set from its bucket if exists
// If the passed tx is already open (not nil), it will use it to get full validator set
// If the passed tx is not open (it is nil), it will open a new transaction on db and get full validator set
func (s *stakeStore) getFullValidatorSet(dbTx *bolt.Tx) (validator.ValidatorSetState, error) {
	var (
		fullValidatorSet validator.ValidatorSetState
		err              error
	)

	getFn := func(tx *bolt.Tx) error {
		raw := tx.Bucket(validatorSetBucket).Get(fullValidatorSetKey)
		if raw == nil {
			return errNoFullValidatorSet
		}

		return fullValidatorSet.Unmarshal(raw)
	}

	if dbTx == nil {
		err = s.db.View(func(tx *bolt.Tx) error {
			return getFn(tx)
		})
	} else {
		err = getFn(dbTx)
	}

	return fullValidatorSet, err
}
