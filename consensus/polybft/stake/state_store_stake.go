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

type StakeStore struct {
	db *bolt.DB
}

func newStakeStore(db *bolt.DB) (*StakeStore, error) {
	var store *StakeStore

	err := db.Update(func(tx *bolt.Tx) error {
		s, err := newStakeStoreWithTx(db, tx)
		if err != nil {
			return err
		}

		store = s

		return nil
	})

	return store, err
}

func newStakeStoreWithTx(db *bolt.DB, dbTx *bolt.Tx) (*StakeStore, error) {
	store := &StakeStore{db: db}

	if _, err := dbTx.CreateBucketIfNotExists(validatorSetBucket); err != nil {
		return nil, fmt.Errorf("failed to create bucket=%s: %w", string(validatorSetBucket), err)
	}

	return store, nil
}

// insertFullValidatorSet inserts full validator set to its bucket (or updates it if exists)
// If the passed tx is already open (not nil), it will use it to insert full validator set
// If the passed tx is not open (it is nil), it will open a new transaction on db and insert full validator set
func (s *StakeStore) insertFullValidatorSet(fullValidatorSet validator.ValidatorSetState, dbTx *bolt.Tx) error {
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
func (s *StakeStore) getFullValidatorSet(dbTx *bolt.Tx) (validator.ValidatorSetState, error) {
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
