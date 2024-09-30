package polybft

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

	// errNotEnoughBridgeEvents error message
	errNotEnoughBridgeEvents = errors.New("there is either a gap or not enough bridge events")
	// errNoBridgeBatchForBridgeEvent error message
	errNoBridgeBatchForBridgeEvent = errors.New("no bridge batch found for given bridge message events")
)

/*
Bolt DB schema:

bridge message events/
|--> chainId --> bridgeMessageEvent.Id -> *BridgeMsgEvent (json marshalled)

bridge batches/
|--> chainId --> bridgeBatches.Message[last].Id -> *BridgeBatchSigned (json marshalled)

relayerEvents/
|--> chainId --> RelayerEventData.EventID -> *RelayerEventData (json marshalled)
*/

type BridgeMessageStore struct {
	db       *bolt.DB
	chainIDs []uint64
}

// initialize creates necessary buckets in DB if they don't already exist
func (bms *BridgeMessageStore) initialize(tx *bolt.Tx) error {
	var err error

	var bridgeMessageBucket, bridgeBatchesBucket *bolt.Bucket

	if bridgeMessageBucket, err = tx.CreateBucketIfNotExists(bridgeMessageEventsBucket); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", string(bridgeMessageEventsBucket), err)
	}

	if bridgeBatchesBucket, err = tx.CreateBucketIfNotExists(bridgeBatchBucket); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", string(bridgeBatchBucket), err)
	}

	for _, chainID := range bms.chainIDs {
		chainIDBytes := common.EncodeUint64ToBytes(chainID)

		if _, err := bridgeMessageBucket.CreateBucketIfNotExists(chainIDBytes); err != nil {
			return fmt.Errorf("failed to create bucket chainID=%s: %w", string(bridgeMessageEventsBucket), err)
		}

		if _, err := bridgeBatchesBucket.CreateBucketIfNotExists(chainIDBytes); err != nil {
			return fmt.Errorf("failed to create bucket chainID=%s: %w", string(bridgeBatchBucket), err)
		}
	}

	return nil
}

// insertBridgeMessageEvent inserts a new bridge message event to state event bucket in db
func (bms *BridgeMessageStore) insertBridgeMessageEvent(event *contractsapi.BridgeMsgEvent) error {
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
func (bms *BridgeMessageStore) removeBridgeEvents(
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
func (bms *BridgeMessageStore) list() ([]*contractsapi.BridgeMsgEvent, error) {
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
func (bms *BridgeMessageStore) getBridgeMessageEventsForBridgeBatch(
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
func (bms *BridgeMessageStore) getBridgeBatchForBridgeEvents(
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
func (bms *BridgeMessageStore) insertBridgeBatchMessage(signedBridgeBatch *BridgeBatchSigned,
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
func (bms *BridgeMessageStore) insertConsensusData(epoch uint64, key []byte,
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
func (bms *BridgeMessageStore) getMessageVotes(
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
func (bms *BridgeMessageStore) getMessageVotesLocked(tx *bolt.Tx, epoch uint64,
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
