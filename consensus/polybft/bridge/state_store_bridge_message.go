package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/helper/common"
	bolt "go.etcd.io/bbolt"
)

var (
	// bucket to store bridge events
	bridgeMessageEventsBucket = []byte("bridgeMessageEvents")
	// bucket to store bridge buckets
	bridgeBatchBucket = []byte("bridgeBatches")
	// bucket to store message votes (signatures)
	messageVotesBucket = []byte("votes")
	// bucket to store epochs and all its nested buckets (message votes and message pool events)
	epochsBucket = []byte("epochs")

	// errNotEnoughBridgeEvents error message
	errNotEnoughBridgeEvents = errors.New("there is either a gap or not enough bridge events")
	// errNoBridgeBatchForBridgeEvent error message
	errNoBridgeBatchForBridgeEvent = errors.New("no bridge batch found for given bridge message events")
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

/*
Bolt DB schema:

bridge message events/
|--> chainId --> bridgeMessageEvent.Id -> *BridgeMsgEvent (json marshalled)

bridge batches/
|--> chainId --> bridgeBatches.Message[last].Id -> *BridgeBatchSigned (json marshalled)

bridge message votes /
|--> chainId --> epoch -> hash -> *BridgeBatchVote (json marshalled)
*/

type BridgeManagerStore struct {
	db       *bolt.DB
	chainIDs []uint64
}

func newBridgeManagerStore(db *bolt.DB, dbTx *bolt.Tx, chainIDs []uint64) (*BridgeManagerStore, error) {
	var err error

	store := &BridgeManagerStore{db: db, chainIDs: chainIDs}

	initFn := func(tx *bolt.Tx) error {
		var bridgeMessageBucket, bridgeBatchesBucket, epochBucket *bolt.Bucket

		if bridgeMessageBucket, err = tx.CreateBucketIfNotExists(bridgeMessageEventsBucket); err != nil {
			return fmt.Errorf("failed to create bucket=%s: %w", string(bridgeMessageEventsBucket), err)
		}

		if bridgeBatchesBucket, err = tx.CreateBucketIfNotExists(bridgeBatchBucket); err != nil {
			return fmt.Errorf("failed to create bucket=%s: %w", string(bridgeBatchBucket), err)
		}

		if epochBucket, err = tx.CreateBucketIfNotExists(epochsBucket); err != nil {
			return fmt.Errorf("failed to create bucket=%s: %w", string(epochsBucket), err)
		}

		for _, chainID := range chainIDs {
			chainIDBytes := common.EncodeUint64ToBytes(chainID)

			if _, err := bridgeMessageBucket.CreateBucketIfNotExists(chainIDBytes); err != nil {
				return fmt.Errorf("failed to create bucket chainID=%s: %w", string(bridgeMessageEventsBucket), err)
			}

			if _, err := bridgeBatchesBucket.CreateBucketIfNotExists(chainIDBytes); err != nil {
				return fmt.Errorf("failed to create bucket chainID=%s: %w", string(bridgeBatchBucket), err)
			}

			if _, err := epochBucket.CreateBucketIfNotExists(chainIDBytes); err != nil {
				return fmt.Errorf("failed to create bucket chainID=%s: %w", string(epochsBucket), err)
			}
		}

		return nil
	}

	if dbTx == nil {
		err = db.Update(initFn)
	} else {
		err = initFn(dbTx)
	}

	return store, err
}

// insertBridgeMessageEvent inserts a new bridge message event to state event bucket in db
func (bms *BridgeManagerStore) insertBridgeMessageEvent(event *contractsapi.BridgeMsgEvent) error {
	return bms.db.Update(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}

		bucket := tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(event.SourceChainID.Uint64()))

		return bucket.Put(common.EncodeUint64ToBytes(event.ID.Uint64()), raw)
	})
}

// removeBridgeEvents removes bridge events and their proofs from the buckets in db
func (bms *BridgeManagerStore) removeBridgeEvents(
	bridgeMessageResult contractsapi.BridgeMessageResultEvent) error {
	return bms.db.Update(func(tx *bolt.Tx) error {
		eventsBucket := tx.Bucket(bridgeMessageEventsBucket).
			Bucket(common.EncodeUint64ToBytes(bridgeMessageResult.SourceChainID.Uint64()))

		bridgeMessageID := bridgeMessageResult.Counter.Uint64()
		bridgeMessageEventIDKey := common.EncodeUint64ToBytes(bridgeMessageID)

		if err := eventsBucket.Delete(bridgeMessageEventIDKey); err != nil {
			return fmt.Errorf("failed to remove bridge message event (ID=%d): %w", bridgeMessageID, err)
		}

		return nil
	})
}

// list iterates through all events in events bucket in db, un-marshals them, and returns as array
func (bms *BridgeManagerStore) list() ([]*contractsapi.BridgeMsgEvent, error) {
	events := []*contractsapi.BridgeMsgEvent{}

	for _, chainID := range bms.chainIDs {
		err := bms.db.View(func(tx *bolt.Tx) error {
			return tx.Bucket(bridgeMessageEventsBucket).
				Bucket(common.EncodeUint64ToBytes(chainID)).ForEach(func(k, v []byte) error {
				var event *contractsapi.BridgeMsgEvent
				if err := json.Unmarshal(v, &event); err != nil {
					return err
				}

				events = append(events, event)

				return nil
			})
		})
		if err != nil {
			return nil, err
		}
	}

	return events, nil
}

// getBridgeMessageEventsForBridgeBatch returns bridge events for bridge batch
func (bms *BridgeManagerStore) getBridgeMessageEventsForBridgeBatch(
	fromIndex, toIndex uint64, dbTx *bolt.Tx, sourceChainID, destinationChainID uint64) (
	[]*contractsapi.BridgeMsgEvent, error) {
	var (
		events []*contractsapi.BridgeMsgEvent
		err    error
	)

	getFn := func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(sourceChainID))
		for i := fromIndex; i <= toIndex; i++ {
			v := bucket.Get(common.EncodeUint64ToBytes(i))
			if v == nil {
				return errNotEnoughBridgeEvents
			}

			var event *contractsapi.BridgeMsgEvent
			if err := json.Unmarshal(v, &event); err != nil {
				return err
			}

			if destinationChainID == 0 ||
				event.DestinationChainID.Cmp(new(big.Int).SetUint64(destinationChainID)) == 0 {
				events = append(events, event)
			}
		}

		return nil
	}

	if dbTx == nil {
		err = bms.db.View(func(tx *bolt.Tx) error {
			return getFn(tx)
		})
	} else {
		err = getFn(dbTx)
	}

	return events, err
}

// getBridgeBatchForBridgeEvents returns the bridgeBatch that contains given bridge event if it exists
func (bms *BridgeManagerStore) getBridgeBatchForBridgeEvents(
	bridgeMessageID,
	chainID uint64) (*BridgeBatchSigned, error) {
	var signedBridgeBatch *BridgeBatchSigned

	err := bms.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bridgeBatchBucket).Bucket(common.EncodeUint64ToBytes(chainID)).Cursor()

		k, v := c.Seek(common.EncodeUint64ToBytes(bridgeMessageID))
		if k == nil {
			return errNoBridgeBatchForBridgeEvent
		}

		if err := json.Unmarshal(v, &signedBridgeBatch); err != nil {
			return err
		}

		if !signedBridgeBatch.ContainsBridgeMessage(bridgeMessageID) {
			return errNoBridgeBatchForBridgeEvent
		}

		return nil
	})

	return signedBridgeBatch, err
}

// insertBridgeBatchMessage inserts signed batch to db
func (bms *BridgeManagerStore) insertBridgeBatchMessage(signedBridgeBatch *BridgeBatchSigned,
	dbTx *bolt.Tx) error {
	insertFn := func(tx *bolt.Tx) error {
		raw, err := json.Marshal(signedBridgeBatch)
		if err != nil {
			return err
		}

		length := len(signedBridgeBatch.MessageBatch.Messages)

		var lastID = uint64(0)

		if length > 0 {
			lastID = signedBridgeBatch.MessageBatch.Messages[length-1].ID.Uint64()
		}

		if err := tx.Bucket(bridgeBatchBucket).
			Bucket(common.EncodeUint64ToBytes(signedBridgeBatch.MessageBatch.SourceChainID.Uint64())).Put(
			common.EncodeUint64ToBytes(lastID), raw); err != nil {
			return err
		}

		return nil
	}

	if dbTx == nil {
		return bms.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	}

	return insertFn(dbTx)
}

// insertConsensusData inserts given batch consensus data to corresponding bucket of given epoch
func (bms *BridgeManagerStore) insertConsensusData(epoch uint64, key []byte,
	vote *BridgeBatchVoteConsensusData, dbTx *bolt.Tx, sourceChainID uint64) (int, error) {
	var (
		numOfSignatures int
		err             error
	)

	insertFn := func(tx *bolt.Tx) error {
		signatures, err := bms.getMessageVotesLocked(tx, epoch, key, sourceChainID)
		if err != nil {
			return err
		}

		// check if the signature has already being included
		for _, sigs := range signatures {
			if sigs.Sender == vote.Sender {
				return nil
			}
		}

		if signatures == nil {
			signatures = []*BridgeBatchVoteConsensusData{vote}
		} else {
			signatures = append(signatures, vote)
		}

		raw, err := json.Marshal(signatures)
		if err != nil {
			return err
		}

		bucket, err := getNestedBucketInEpoch(tx, epoch, messageVotesBucket, sourceChainID)
		if err != nil {
			return err
		}

		numOfSignatures = len(signatures)

		return bucket.Put(key, raw)
	}

	if dbTx == nil {
		err = bms.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	} else {
		err = insertFn(dbTx)
	}

	return numOfSignatures, err
}

// getMessageVotes gets all signatures from db associated with given epoch and hash
func (bms *BridgeManagerStore) getMessageVotes(
	epoch uint64,
	hash []byte,
	sourceChainID uint64) ([]*BridgeBatchVoteConsensusData, error) {
	var signatures []*BridgeBatchVoteConsensusData

	err := bms.db.View(func(tx *bolt.Tx) error {
		res, err := bms.getMessageVotesLocked(tx, epoch, hash, sourceChainID)
		if err != nil {
			return err
		}

		signatures = res

		return nil
	})

	if err != nil {
		return nil, err
	}

	return signatures, nil
}

// getMessageVotesLocked gets all signatures from db associated with given epoch and hash
func (bms *BridgeManagerStore) getMessageVotesLocked(tx *bolt.Tx, epoch uint64,
	hash []byte, sourceChainID uint64) ([]*BridgeBatchVoteConsensusData, error) {
	bucket, err := getNestedBucketInEpoch(tx, epoch, messageVotesBucket, sourceChainID)
	if err != nil {
		return nil, err
	}

	v := bucket.Get(hash)
	if v == nil {
		return nil, nil
	}

	var signatures []*BridgeBatchVoteConsensusData
	if err := json.Unmarshal(v, &signatures); err != nil {
		return nil, err
	}

	return signatures, nil
}

// getNestedBucketInEpoch returns a nested (child) bucket from db associated with given epoch
func getNestedBucketInEpoch(tx *bolt.Tx, epoch uint64, bucketKey []byte, chainID uint64) (*bolt.Bucket, error) {
	epochBucket, err := getEpochBucket(tx, epoch, chainID)
	if err != nil {
		return nil, err
	}

	bucket := epochBucket.Bucket(bucketKey)
	if bucket == nil {
		return nil, fmt.Errorf("could not find %v bucket for epoch: %v", string(bucketKey), epoch)
	}

	return bucket, nil
}

// getEpochBucket returns bucket from db associated with given epoch
func getEpochBucket(tx *bolt.Tx, epoch uint64, chainID uint64) (*bolt.Bucket, error) {
	epochBucket := tx.Bucket(epochsBucket).
		Bucket(common.EncodeUint64ToBytes(chainID)).
		Bucket(common.EncodeUint64ToBytes(epoch))
	if epochBucket == nil {
		return nil, fmt.Errorf("could not find bucket for epoch: %v", epoch)
	}

	return epochBucket, nil
}

// insertEpoch inserts a new epoch to db with its meta data
func (bms *BridgeManagerStore) insertEpoch(epoch uint64, dbTx *bolt.Tx, chainID uint64) error {
	insertFn := func(tx *bolt.Tx) error {
		chainIDBucket, err := tx.Bucket(epochsBucket).CreateBucketIfNotExists(common.EncodeUint64ToBytes(chainID))
		if err != nil {
			return err
		}

		epochBucket, err := chainIDBucket.CreateBucketIfNotExists(common.EncodeUint64ToBytes(epoch))
		if err != nil {
			return err
		}

		_, err = epochBucket.CreateBucketIfNotExists(messageVotesBucket)
		if err != nil {
			return err
		}

		return err
	}

	if dbTx == nil {
		return bms.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	}

	return insertFn(dbTx)
}

// cleanEpochsFromDB cleans epoch buckets from db
func (bms *BridgeManagerStore) cleanEpochsFromDB(dbTx *bolt.Tx) error {
	cleanFn := func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(epochsBucket); err != nil {
			return err
		}

		epochBucket, err := tx.CreateBucket(epochsBucket)
		if err != nil {
			return err
		}

		for _, chainID := range bms.chainIDs {
			if _, err := epochBucket.CreateBucket(common.EncodeUint64ToBytes(chainID)); err != nil {
				return err
			}
		}

		return nil
	}

	if dbTx == nil {
		return bms.db.Update(func(tx *bolt.Tx) error {
			return cleanFn(tx)
		})
	}

	return cleanFn(dbTx)
}
