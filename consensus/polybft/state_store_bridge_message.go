package polybft

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/helper/common"
	bolt "go.etcd.io/bbolt"
)

var (
	// bucket to store rootchain bridge events
	bridgeMessageEventsBucket = []byte("bridgeMessageEvents")
	// bucket to store bridge buckets
	bridgeBatchBucket = []byte("bridgeBatches")
	// bucket to store state sync proofs
	bridgeMessageProofsBucket = []byte("stateSyncProofs")
	// bucket to store message votes (signatures)
	messageVotesBucket = []byte("votes")
	// bucket to store all state sync relayer events
	stateSyncRelayerEventsBucket = []byte("stateSyncRelayerEvents")

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

stateSyncProofs/
|--> chainId --> stateSyncProof.StateSync.Id -> *StateSyncProof (json marshalled)

relayerEvents/
|--> chainId --> RelayerEventData.EventID -> *RelayerEventData (json marshalled)
*/

type BridgeMessageStore struct {
	db       *bolt.DB
	chainIDs []uint64
}

// initialize creates necessary buckets in DB if they don't already exist
func (s *BridgeMessageStore) initialize(tx *bolt.Tx) error {
	var err error
	var bridgeMessageBucket, bridgeBatchesBucket, bridgeMessageProofBucket, stateSyncRelayerBucket *bolt.Bucket

	if bridgeMessageBucket, err = tx.CreateBucketIfNotExists(bridgeMessageEventsBucket); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", bridgeMessageEventsBucket, err)
	}

	if bridgeBatchesBucket, err = tx.CreateBucketIfNotExists([]byte(bridgeBatchBucket)); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", string(bridgeBatchBucket), err)
	}

	if bridgeMessageProofBucket, err = tx.CreateBucketIfNotExists([]byte(bridgeMessageProofsBucket)); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", string(bridgeMessageProofsBucket), err)
	}

	if stateSyncRelayerBucket, err = tx.CreateBucketIfNotExists([]byte(stateSyncRelayerEventsBucket)); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", string(stateSyncRelayerEventsBucket), err)
	}

	for _, chainId := range s.chainIDs {
		chainIdBytes := common.EncodeUint64ToBytes(chainId)

		if _, err := bridgeMessageBucket.CreateBucketIfNotExists(chainIdBytes); err != nil {
			return fmt.Errorf("failed to create bucket chainID=%s: %w", string(bridgeMessageEventsBucket), err)
		}

		if _, err := bridgeBatchesBucket.CreateBucketIfNotExists(chainIdBytes); err != nil {
			return fmt.Errorf("failed to create bucket chainID=%s: %w", string(bridgeBatchBucket), err)
		}

		if _, err := bridgeMessageProofBucket.CreateBucketIfNotExists(chainIdBytes); err != nil {
			return fmt.Errorf("failed to create bucket chainID=%s: %w", string(bridgeMessageProofsBucket), err)
		}

		if _, err := stateSyncRelayerBucket.CreateBucketIfNotExists(chainIdBytes); err != nil {
			return fmt.Errorf("failed to create bucket chainID=%s: %w", string(stateSyncRelayerEventsBucket), err)
		}
	}

	return nil
}

// insertBridgeMessageEvent inserts a new bridge message event to state event bucket in db
func (s *BridgeMessageStore) insertBridgeMessageEvent(event *contractsapi.BridgeMsgEvent) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}

		bucket := tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(event.DestinationChainID.Uint64()))

		return bucket.Put(common.EncodeUint64ToBytes(event.ID.Uint64()), raw)
	})
}

// removeBridgeEventsAndProofs remove bridge events and their proofs from the buckets in db
func (s *BridgeMessageStore) removeBridgeEventsAndProofs(bridgeMessageEventIDs *contractsapi.BridgeMessageResultEvent) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		eventsBucket := tx.Bucket(bridgeMessageEventsBucket)
		proofsBucket := tx.Bucket(bridgeMessageProofsBucket)

		bridgeMessageID := bridgeMessageEventIDs.Counter.Uint64()

		bridgeMessageEventIDKey := common.EncodeUint64ToBytes(bridgeMessageID)

		if err := eventsBucket.Delete(bridgeMessageEventIDKey); err != nil {
			return fmt.Errorf("failed to remove state sync event (ID=%d): %w", bridgeMessageID, err)
		}

		if err := proofsBucket.Delete(bridgeMessageEventIDKey); err != nil {
			return fmt.Errorf("failed to remove state sync event proof (ID=%d): %w", bridgeMessageID, err)
		}

		return nil
	})
}

// list iterates through all events in events bucket in db, un-marshals them, and returns as array
func (s *BridgeMessageStore) list() ([]*contractsapi.BridgeMsgEvent, error) {
	events := []*contractsapi.BridgeMsgEvent{}

	for _, chainID := range s.chainIDs {
		err := s.db.View(func(tx *bolt.Tx) error {
			return tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(chainID)).ForEach(func(k, v []byte) error {
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
func (s *BridgeMessageStore) getBridgeMessageEventsForBridgeBatch(
	fromIndex, toIndex uint64, dbTx *bolt.Tx, destinationChainID uint64) ([]*contractsapi.BridgeMsgEvent, error) {
	var (
		events []*contractsapi.BridgeMsgEvent
		err    error
	)

	getFn := func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(destinationChainID))
		for i := fromIndex; i <= toIndex; i++ {
			v := bucket.Get(common.EncodeUint64ToBytes(i))
			if v == nil {
				return errNotEnoughBridgeEvents
			}

			var event *contractsapi.BridgeMsgEvent
			if err := json.Unmarshal(v, &event); err != nil {
				return err
			}

			events = append(events, event)
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

	return events, err
}

// getBridgeBatchForBridgeEvents returns the bridgeBatch that contains given bridge event if it exists
func (s *BridgeMessageStore) getBridgeBatchForBridgeEvents(bridgeMessageID, chainId uint64) (*BridgeBatchSigned, error) {
	var commitment *BridgeBatchSigned

	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bridgeBatchBucket).Bucket(common.EncodeUint64ToBytes(chainId)).Cursor()

		k, v := c.Seek(common.EncodeUint64ToBytes(bridgeMessageID))
		if k == nil {
			return errNoBridgeBatchForBridgeEvent
		}

		if err := json.Unmarshal(v, &commitment); err != nil {
			return err
		}

		if !commitment.ContainsBridgeMessage(bridgeMessageID) {
			return errNoBridgeBatchForBridgeEvent
		}

		return nil
	})

	return commitment, err
}

// insertBridgeBatchMessage inserts signed commitment to db
func (s *BridgeMessageStore) insertBridgeBatchMessage(commitment *BridgeBatchSigned,
	dbTx *bolt.Tx) error {
	insertFn := func(tx *bolt.Tx) error {
		raw, err := json.Marshal(commitment)
		if err != nil {
			return err
		}

		length := len(commitment.MessageBatch.Messages)
		var lastId = uint64(0)

		if length > 0 {
			lastId = commitment.MessageBatch.Messages[length-1].ID.Uint64()
		}

		if err := tx.Bucket(bridgeBatchBucket).Bucket(common.EncodeUint64ToBytes(commitment.MessageBatch.DestinationChainID.Uint64())).Put(
			common.EncodeUint64ToBytes(lastId), raw); err != nil {
			return err
		}

		return nil
	}

	if dbTx == nil {
		return s.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	}

	return insertFn(dbTx)
}

// getBridgeBatchSigned queries the signed bridge batch from the db
func (s *BridgeMessageStore) getBridgeBatchSigned(toIndex uint64, chainId uint64) (*BridgeBatchSigned, error) {
	var commitment *BridgeBatchSigned

	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bridgeBatchBucket).Bucket(common.EncodeUint64ToBytes(chainId)).Get(common.EncodeUint64ToBytes(toIndex))
		if raw == nil {
			return nil
		}

		return json.Unmarshal(raw, &commitment)
	})

	return commitment, err
}

// insertMessageVote inserts given vote to signatures bucket of given epoch
func (s *BridgeMessageStore) insertMessageVote(epoch uint64, key []byte,
	vote *MessageSignature, dbTx *bolt.Tx, destinationChainID uint64) (int, error) {
	var (
		numOfSignatures int
		err             error
	)

	insertFn := func(tx *bolt.Tx) error {
		signatures, err := s.getMessageVotesLocked(tx, epoch, key, destinationChainID)
		if err != nil {
			return err
		}

		// check if the signature has already being included
		for _, sigs := range signatures {
			if sigs.From == vote.From {
				return nil
			}
		}

		if signatures == nil {
			signatures = []*MessageSignature{vote}
		} else {
			signatures = append(signatures, vote)
		}

		raw, err := json.Marshal(signatures)
		if err != nil {
			return err
		}

		bucket, err := getNestedBucketInEpoch(tx, epoch, messageVotesBucket, destinationChainID)
		if err != nil {
			return err
		}

		numOfSignatures = len(signatures)

		return bucket.Put(key, raw)
	}

	if dbTx == nil {
		err = s.db.Update(func(tx *bolt.Tx) error {
			return insertFn(tx)
		})
	} else {
		err = insertFn(dbTx)
	}

	return numOfSignatures, err
}

// getMessageVotes gets all signatures from db associated with given epoch and hash
func (s *BridgeMessageStore) getMessageVotes(epoch uint64, hash []byte, destinationChainID uint64) ([]*MessageSignature, error) {
	var signatures []*MessageSignature

	err := s.db.View(func(tx *bolt.Tx) error {
		res, err := s.getMessageVotesLocked(tx, epoch, hash, destinationChainID)
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
func (s *BridgeMessageStore) getMessageVotesLocked(tx *bolt.Tx, epoch uint64,
	hash []byte, destinationChainID uint64) ([]*MessageSignature, error) {
	bucket, err := getNestedBucketInEpoch(tx, epoch, messageVotesBucket, destinationChainID)
	if err != nil {
		return nil, err
	}

	v := bucket.Get(hash)
	if v == nil {
		return nil, nil
	}

	var signatures []*MessageSignature
	if err := json.Unmarshal(v, &signatures); err != nil {
		return nil, err
	}

	return signatures, nil
}

// updateRelayerEvents updates/remove desired state sync relayer events
func (s *BridgeMessageStore) UpdateRelayerEvents(
	events []*RelayerEventMetaData, removeIDs []*RelayerEventMetaData, dbTx *bolt.Tx) error {
	return updateRelayerEvents(stateSyncRelayerEventsBucket, events, removeIDs, s.db, dbTx)
}

// getAllAvailableRelayerEvents retrieves all StateSync RelayerEventData that should be sent as a transactions
func (s *BridgeMessageStore) GetAllAvailableRelayerEvents(limit int) (result []*RelayerEventMetaData, err error) {
	for _, chainId := range s.chainIDs {
		if err = s.db.View(func(tx *bolt.Tx) error {
			result, err = getAvailableRelayerEvents(limit, stateSyncRelayerEventsBucket, tx, chainId)
			if err != nil {
				return err
			}

			return nil
		}); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// getAvailableRelayerEvents retrieves all relayer that should be sent as a transactions
func getAvailableRelayerEvents(limit int, bucket []byte, tx *bolt.Tx, chainId uint64) (result []*RelayerEventMetaData, err error) {
	cursor := tx.Bucket(bucket).Bucket(common.EncodeUint64ToBytes(chainId)).Cursor()

	for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
		var event *RelayerEventMetaData

		if err = json.Unmarshal(v, &event); err != nil {
			return
		}

		result = append(result, event)

		if limit > 0 && len(result) >= limit {
			break
		}
	}

	return
}

// updateRelayerEvents updates/remove desired relayer events
func updateRelayerEvents(
	bucket []byte,
	events []*RelayerEventMetaData,
	removeIDs []*RelayerEventMetaData,
	db *bolt.DB,
	openedTx *bolt.Tx) error {
	updateFn := func(tx *bolt.Tx) error {

		for _, event := range events {
			relayerEventsBucket := tx.Bucket(bucket).Bucket(common.EncodeUint64ToBytes(event.DestinationChainID))

			raw, err := json.Marshal(event)
			if err != nil {
				return err
			}

			key := common.EncodeUint64ToBytes(event.EventID)

			if err := relayerEventsBucket.Put(key, raw); err != nil {
				return err
			}
		}

		for _, event := range removeIDs {
			relayerEventsBucket := tx.Bucket(bucket).Bucket(common.EncodeUint64ToBytes(event.DestinationChainID))
			eventIDKey := common.EncodeUint64ToBytes(event.EventID)

			if err := relayerEventsBucket.Delete(eventIDKey); err != nil {
				return fmt.Errorf("failed to remove relayer event (ID=%d): %w", event.EventID, err)
			}
		}

		return nil
	}

	if openedTx == nil {
		return db.Update(func(tx *bolt.Tx) error {
			return updateFn(tx)
		})
	}

	return updateFn(openedTx)
}
