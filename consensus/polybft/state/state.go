package state

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/helper/common"
	bolt "go.etcd.io/bbolt"
)

var (
	edgeEventsLastProcessedBlockBucket = []byte("EdgeEventsLastProcessedBlock")
	edgeEventsLastProcessedBlockKey    = []byte("EdgeEventsLastProcessedBlockKey")
)

// BridgeBatchVoteConsensusData encapsulates sender identifier and its signature
type BridgeBatchVoteConsensusData struct {
	// Signer of the vote
	Sender string
	// Signature of the message
	Signature []byte
}

// BridgeBatchVote represents the payload which is gossiped across the network
type BridgeBatchVote struct {
	*BridgeBatchVoteConsensusData
	// Hash is encoded data
	Hash []byte
	// Number of epoch
	EpochNumber uint64
	// SourceChainID from bridge batch
	SourceChainID uint64
	// DestinationChainID from bridge batch
	DestinationChainID uint64
}

// State represents a persistence layer which persists consensus data off-chain
type State struct {
	db    *bolt.DB
	close chan struct{}
}

// NewState creates new instance of State
func NewState(path string, closeCh chan struct{}) (*State, error) {
	db, err := bolt.Open(path, 0666, nil)
	if err != nil {
		return nil, err
	}

	s := &State{
		db:    db,
		close: closeCh,
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(edgeEventsLastProcessedBlockBucket); err != nil {
			return fmt.Errorf("cannot create bucket: %w", err)
		}

		return nil
	})

	return s, err
}

// InsertLastProcessedEventsBlock inserts the last processed block for events on Edge
func (s *State) InsertLastProcessedEventsBlock(block uint64, dbTx *bolt.Tx) error {
	insertFn := func(tx *bolt.Tx) error {
		return tx.Bucket(edgeEventsLastProcessedBlockBucket).Put(
			edgeEventsLastProcessedBlockKey, common.EncodeUint64ToBytes(block))
	}

	if dbTx == nil {
		return s.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	}

	return insertFn(dbTx)
}

// GetLastProcessedEventsBlock gets the last processed block for events on Edge
func (s *State) GetLastProcessedEventsBlock(dbTx *bolt.Tx) (uint64, error) {
	var (
		lastProcessed uint64
		err           error
	)

	getFn := func(tx *bolt.Tx) {
		value := tx.Bucket(edgeEventsLastProcessedBlockBucket).Get(edgeEventsLastProcessedBlockKey)
		if value != nil {
			lastProcessed = common.EncodeBytesToUint64(value)
		}
	}

	if dbTx == nil {
		err = s.db.View(func(tx *bolt.Tx) error {
			getFn(tx)

			return nil
		})
	} else {
		getFn(dbTx)
	}

	return lastProcessed, err
}

func (s *State) DB() *bolt.DB {
	return s.db
}

// Close closes the state
func (s *State) Close() error {
	return s.db.Close()
}

// BeginDBTransaction creates and begins a transaction on BoltDB
// Note that transaction needs to be manually rollback or committed
func (s *State) BeginDBTransaction(isWriteTx bool) (*bolt.Tx, error) {
	return s.db.Begin(isWriteTx)
}

// BucketStats returns stats for the given bucket in db
func BucketStats(bucketName []byte, db *bolt.DB) (*bolt.BucketStats, error) {
	var stats *bolt.BucketStats

	err := db.View(func(tx *bolt.Tx) error {
		s := tx.Bucket(bucketName).Stats()
		stats = &s

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("cannot check bucket stats. Bucket name=%s: %w", string(bucketName), err)
	}

	return stats, nil
}
