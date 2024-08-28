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
	bridgeMessageEventsBucket = []byte("stateSyncEvents")
	// bucket to store commitments
	commitmentsBucket = []byte("commitments")
	// bucket to store state sync proofs
	bridgeMessageProofsBucket = []byte("stateSyncProofs")
	// bucket to store message votes (signatures)
	messageVotesBucket = []byte("votes")
	// bucket to store all state sync relayer events
	stateSyncRelayerEventsBucket = []byte("stateSyncRelayerEvents")

	// errNotEnoughStateSyncs error message
	errNotEnoughStateSyncs = errors.New("there is either a gap or not enough sync events")
	// errNoCommitmentForStateSync error message
	errNoCommitmentForStateSync = errors.New("no commitment found for given state sync event")
)

/*
Bolt DB schema:

state sync events/
|--> stateSyncEvent.Id -> *StateSyncEvent (json marshalled)

commitments/
|--> commitment.Message.ToIndex -> *CommitmentMessageSigned (json marshalled)

stateSyncProofs/
|--> stateSyncProof.StateSync.Id -> *StateSyncProof (json marshalled)

relayerEvents/
|--> RelayerEventData.EventID -> *RelayerEventData (json marshalled)
*/

type BridgeMessageStore struct {
	db       *bolt.DB
	chainIDs []uint64
}

// initialize creates necessary buckets in DB if they don't already exist
func (s *BridgeMessageStore) initialize(tx *bolt.Tx) error {
	var err error
	var bridgeMessageBucket, commitmentBucket, bridgeMessageProofBucket, stateSyncRelayerBucket *bolt.Bucket

	if bridgeMessageBucket, err = tx.CreateBucketIfNotExists(bridgeMessageEventsBucket); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", bridgeMessageEventsBucket, err)
	}

	if commitmentBucket, err = tx.CreateBucketIfNotExists([]byte(commitmentsBucket)); err != nil {
		return fmt.Errorf("failed to create bucket=%s: %w", string(commitmentsBucket), err)
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

		if _, err := commitmentBucket.CreateBucketIfNotExists(chainIdBytes); err != nil {
			return fmt.Errorf("failed to create bucket chainID=%s: %w", string(commitmentsBucket), err)
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

// insertStateSyncEvent inserts a new state sync event to state event bucket in db
func (s *BridgeMessageStore) insertBridgeMessageEvent(event *contractsapi.BridgeMessageEventEvent) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}

		bucket := tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(event.DestinationChainID.Uint64()))

		return bucket.Put(common.EncodeUint64ToBytes(event.ID.Uint64()), raw)
	})
}

// removeStateSyncEventsAndProofs removes state sync events and their proofs from the buckets in db
func (s *BridgeMessageStore) removeStateSyncEventsAndProofs(bridgeMessageEventIDs *contractsapi.BridgeMessageResultEvent) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		eventsBucket := tx.Bucket(bridgeMessageEventsBucket)         //FIX need destinationChainID
		proofsBucket := tx.Bucket([]byte(bridgeMessageProofsBucket)) //FIX need destinationChainID

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
func (s *BridgeMessageStore) list() ([]*contractsapi.BridgeMessageEventEvent, error) {
	events := []*contractsapi.BridgeMessageEventEvent{}

	for _, chainID := range s.chainIDs {
		err := s.db.View(func(tx *bolt.Tx) error {
			return tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(chainID)).ForEach(func(k, v []byte) error {
				var event *contractsapi.BridgeMessageEventEvent
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

// getStateSyncEventsForCommitment returns state sync events for commitment
func (s *BridgeMessageStore) getBridgeMessageEventsForCommitment(
	fromIndex, toIndex uint64, dbTx *bolt.Tx, destinationChainID uint64) ([]*contractsapi.BridgeMessageEventEvent, error) {
	var (
		events []*contractsapi.BridgeMessageEventEvent
		err    error
	)

	getFn := func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bridgeMessageEventsBucket).Bucket(common.EncodeUint64ToBytes(destinationChainID))
		for i := fromIndex; i <= toIndex; i++ {
			v := bucket.Get(common.EncodeUint64ToBytes(i))
			if v == nil {
				return errNotEnoughStateSyncs
			}

			var event *contractsapi.BridgeMessageEventEvent
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

// getCommitmentForStateSync returns the commitment that contains given state sync event if it exists
func (s *BridgeMessageStore) getCommitmentForBridgeEvents(bridgeMessageID, chainId uint64) (*CommitmentMessageSigned, error) {
	var commitment *CommitmentMessageSigned

	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(commitmentsBucket).Bucket(common.EncodeUint64ToBytes(chainId)).Cursor() //FIX

		k, v := c.Seek(common.EncodeUint64ToBytes(bridgeMessageID))
		if k == nil {
			return errNoCommitmentForStateSync
		}

		if err := json.Unmarshal(v, &commitment); err != nil {
			return err
		}

		if !commitment.ContainsBridgeMessage(bridgeMessageID) {
			return errNoCommitmentForStateSync
		}

		return nil
	})

	return commitment, err
}

// insertCommitmentMessage inserts signed commitment to db
func (s *BridgeMessageStore) insertCommitmentMessage(commitment *CommitmentMessageSigned,
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

		if err := tx.Bucket(commitmentsBucket).Bucket(common.EncodeUint64ToBytes(commitment.MessageBatch.DestinationChainID.Uint64())).Put(
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

// getCommitmentMessage queries the signed commitment from the db
func (s *BridgeMessageStore) getCommitmentMessage(toIndex uint64, chainId uint64) (*CommitmentMessageSigned, error) {
	var commitment *CommitmentMessageSigned

	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(commitmentsBucket).Bucket(common.EncodeUint64ToBytes(chainId)).Get(common.EncodeUint64ToBytes(toIndex)) //FIX
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
