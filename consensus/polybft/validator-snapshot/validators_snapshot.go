package validatorsnapshot

import (
	"errors"
	"fmt"
	"sync"

	"github.com/0xPolygon/polygon-edge/blockchain"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

const (
	// ValidatorSnapshotLimit defines a maximum number of validator snapshots
	// that can be stored in cache (both memory and db)
	ValidatorSnapshotLimit = 100
	// NumberOfSnapshotsToLeaveInMemory defines a number of validator snapshots to leave in memory
	NumberOfSnapshotsToLeaveInMemory = 12
	// NumberOfSnapshotsToLeaveInDB defines a number of validator snapshots to leave in db
	NumberOfSnapshotsToLeaveInDB = 20
)

type ValidatorSnapshot struct {
	Epoch            uint64               `json:"epoch"`
	EpochEndingBlock uint64               `json:"epochEndingBlock"`
	Snapshot         validator.AccountSet `json:"snapshot"`
}

func (vs *ValidatorSnapshot) copy() *ValidatorSnapshot {
	copiedAccountSet := vs.Snapshot.Copy()

	return &ValidatorSnapshot{
		Epoch:            vs.Epoch,
		EpochEndingBlock: vs.EpochEndingBlock,
		Snapshot:         copiedAccountSet,
	}
}

type ValidatorsSnapshotCache struct {
	snapshots  map[uint64]*ValidatorSnapshot
	state      *validatorSnapshotStore
	blockchain polychain.Blockchain
	lock       sync.Mutex
	logger     hclog.Logger
}

// NewValidatorsSnapshotCache initializes a new instance of validatorsSnapshotCache
func NewValidatorsSnapshotCache(
	logger hclog.Logger, state *state.State, blockchain polychain.Blockchain,
) (*ValidatorsSnapshotCache, error) {
	vss, err := newValidatorSnapshotStore(state.DB())
	if err != nil {
		return nil, fmt.Errorf("failed to create validator snapshot store: %w", err)
	}

	return &ValidatorsSnapshotCache{
		snapshots:  map[uint64]*ValidatorSnapshot{},
		state:      vss,
		blockchain: blockchain,
		logger:     logger.Named("validators_snapshot"),
	}, nil
}

// GetSnapshot tries to retrieve the most recent cached snapshot (if any) and
// applies pending validator set deltas to it.
// Otherwise, it builds a snapshot from scratch and applies pending validator set deltas.
func (v *ValidatorsSnapshotCache) GetSnapshot(
	blockNumber uint64, parents []*types.Header, dbTx *bolt.Tx) (validator.AccountSet, error) {
	tx := dbTx
	isPassedTxNil := dbTx == nil

	if isPassedTxNil {
		// if no tx is passed, we need to open one, because,
		// GetSnapshot is called from multiple routines (new sequence, fsm, OnBlockInserted, etc)
		// and what can happen is that one routine takes its lock, and without a global tx,
		// a deadlock can happen between that routine and OnBlockInserted, because it to
		// is calling GetSnapshot, but it also opens a global db tx, resulting in a deadlock
		// between one routine waiting to get a transaction on db, and OnBlockInserted waiting
		// to get the validatorsSnapshotCache lock
		t, err := v.state.beginDBTransaction(true)
		if err != nil {
			return nil, err
		}

		tx = t
		defer tx.Rollback() //nolint:errcheck
	}

	v.lock.Lock()
	defer func() {
		v.lock.Unlock()
	}()

	_, extra, err := helpers.GetBlockData(blockNumber, v.blockchain)
	if err != nil {
		return nil, err
	}

	isEpochEndingBlock, err := isEpochEndingBlock(blockNumber, extra, v.blockchain)
	if err != nil && !errors.Is(err, blockchain.ErrNoBlock) {
		// if there is no block after given block, we assume its not epoch ending block
		// but, it's a regular use case, and we should not stop the snapshot calculation
		// because there are cases we need the snapshot for the latest block in chain
		return nil, err
	}

	epochToGetSnapshot := extra.BlockMetaData.EpochNumber
	if !isEpochEndingBlock {
		epochToGetSnapshot--
	}

	v.logger.Trace("Retrieving snapshot started...", "Block", blockNumber, "Epoch", epochToGetSnapshot)

	latestValidatorSnapshot, err := v.getLastCachedSnapshot(epochToGetSnapshot, tx)
	if err != nil {
		return nil, err
	}

	if latestValidatorSnapshot != nil && latestValidatorSnapshot.Epoch == epochToGetSnapshot {
		// we have snapshot for required block (epoch) in cache
		return latestValidatorSnapshot.Snapshot, nil
	}

	if latestValidatorSnapshot == nil {
		// Haven't managed to retrieve snapshot for any epoch from the cache.
		// Build snapshot from the scratch, by applying delta from the genesis block.
		genesisBlockSnapshot, err := v.computeSnapshot(nil, 0, parents)
		if err != nil {
			return nil, fmt.Errorf("failed to compute snapshot for epoch 0: %w", err)
		}

		err = v.StoreSnapshot(genesisBlockSnapshot, tx)
		if err != nil {
			return nil, fmt.Errorf("failed to store validators snapshot for epoch 0: %w", err)
		}

		latestValidatorSnapshot = genesisBlockSnapshot

		v.logger.Trace("Built validators snapshot for genesis block")
	}

	deltasCount := 0

	v.logger.Trace("Applying deltas started...", "LatestSnapshotEpoch", latestValidatorSnapshot.Epoch)

	// Create the snapshot for the desired block (epoch) by incrementally applying deltas to the latest stored snapshot
	for latestValidatorSnapshot.Epoch < epochToGetSnapshot {
		nextEpochEndBlockNumber, err := v.getNextEpochEndingBlock(latestValidatorSnapshot.EpochEndingBlock)
		if err != nil {
			return nil, fmt.Errorf("failed to get the epoch ending block for epoch: %d. Error: %w",
				latestValidatorSnapshot.Epoch+1, err)
		}

		v.logger.Trace("Applying delta", "epochEndBlock", nextEpochEndBlockNumber)

		intermediateSnapshot, err := v.computeSnapshot(latestValidatorSnapshot, nextEpochEndBlockNumber, parents)
		if err != nil {
			return nil, fmt.Errorf("failed to compute snapshot for epoch %d: %w", latestValidatorSnapshot.Epoch+1, err)
		}

		latestValidatorSnapshot = intermediateSnapshot
		if err = v.StoreSnapshot(latestValidatorSnapshot, tx); err != nil {
			return nil, fmt.Errorf("failed to store validators snapshot for epoch %d: %w", latestValidatorSnapshot.Epoch, err)
		}

		deltasCount++
	}

	v.logger.Trace(
		fmt.Sprintf("Applied %d delta(s) to the validators snapshot", deltasCount),
		"Epoch", latestValidatorSnapshot.Epoch,
	)

	if err := v.cleanup(tx); err != nil {
		// error on cleanup should not block or fail any action
		v.logger.Error("could not clean validator snapshots from cache and db", "err", err)
	}

	if isPassedTxNil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	return latestValidatorSnapshot.Snapshot.Copy(), nil
}

// computeSnapshot gets desired block header by block number, extracts its extra and applies given delta to the snapshot
func (v *ValidatorsSnapshotCache) computeSnapshot(
	existingSnapshot *ValidatorSnapshot,
	nextEpochEndBlockNumber uint64,
	parents []*types.Header,
) (*ValidatorSnapshot, error) {
	var header *types.Header

	v.logger.Trace("Compute snapshot started...", "BlockNumber", nextEpochEndBlockNumber)

	if len(parents) > 0 {
		for i := len(parents) - 1; i >= 0; i-- {
			parentHeader := parents[i]
			if parentHeader.Number == nextEpochEndBlockNumber {
				v.logger.Trace("Compute snapshot. Found header in parents", "Header", parentHeader.Number)
				header = parentHeader

				break
			}
		}
	}

	if header == nil {
		var ok bool

		header, ok = v.blockchain.GetHeaderByNumber(nextEpochEndBlockNumber)
		if !ok {
			return nil, fmt.Errorf("unknown block. Block number=%v", nextEpochEndBlockNumber)
		}
	}

	extra, err := polytypes.GetIbftExtra(header.ExtraData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode extra from the block#%d: %w", header.Number, err)
	}

	var (
		snapshot      validator.AccountSet
		snapshotEpoch uint64
	)

	if existingSnapshot == nil {
		snapshot = validator.AccountSet{}
	} else {
		snapshot = existingSnapshot.Snapshot
		snapshotEpoch = existingSnapshot.Epoch + 1
	}

	snapshot, err = snapshot.ApplyDelta(extra.Validators)
	if err != nil {
		return nil, fmt.Errorf("failed to apply delta to the validators snapshot, block#%d: %w", header.Number, err)
	}

	if len(snapshot) == 0 {
		return nil, fmt.Errorf("validator snapshot is empty for block: %d", header.Number)
	}

	v.logger.Debug("Computed snapshot",
		"blockNumber", nextEpochEndBlockNumber,
		"snapshot", snapshot.String(),
		"delta", extra.Validators)

	return &ValidatorSnapshot{
		Epoch:            snapshotEpoch,
		EpochEndingBlock: nextEpochEndBlockNumber,
		Snapshot:         snapshot,
	}, nil
}

// StoreSnapshot stores given snapshot to the in-memory cache and database
func (v *ValidatorsSnapshotCache) StoreSnapshot(snapshot *ValidatorSnapshot, dbTx *bolt.Tx) error {
	v.snapshots[snapshot.Epoch] = snapshot
	if err := v.state.insertValidatorSnapshot(snapshot, dbTx); err != nil {
		return fmt.Errorf("failed to insert validator snapshot for epoch %d to the database: %w", snapshot.Epoch, err)
	}

	v.logger.Trace("Store snapshot", "Snapshots", v.snapshots)

	return nil
}

// Cleanup cleans the validators cache in memory and db
func (v *ValidatorsSnapshotCache) cleanup(dbTx *bolt.Tx) error {
	if len(v.snapshots) >= ValidatorSnapshotLimit {
		latestEpoch := uint64(0)

		for e := range v.snapshots {
			if e > latestEpoch {
				latestEpoch = e
			}
		}

		startEpoch := latestEpoch
		cache := make(map[uint64]*ValidatorSnapshot, NumberOfSnapshotsToLeaveInMemory)

		for i := 0; i < NumberOfSnapshotsToLeaveInMemory; i++ {
			if snapshot, exists := v.snapshots[startEpoch]; exists {
				cache[startEpoch] = snapshot
			}

			startEpoch--
		}

		v.snapshots = cache

		return v.state.cleanValidatorSnapshotsFromDB(latestEpoch, dbTx)
	}

	return nil
}

// getLastCachedSnapshot gets the latest snapshot cached
// If it doesn't have snapshot cached for desired epoch, it will return the latest one it has
func (v *ValidatorsSnapshotCache) getLastCachedSnapshot(currentEpoch uint64,
	dbTx *bolt.Tx) (*ValidatorSnapshot, error) {
	epochToQuery := currentEpoch

	cachedSnapshot := v.snapshots[currentEpoch]
	if cachedSnapshot != nil {
		return cachedSnapshot, nil
	}

	// if we do not have a snapshot in memory for given epoch, we will get the latest one we have
	for {
		cachedSnapshot = v.snapshots[currentEpoch]
		if cachedSnapshot != nil {
			v.logger.Trace("Found snapshot in memory cache", "Epoch", currentEpoch)

			break
		}

		if currentEpoch == 0 { // prevent uint64 underflow
			break
		}

		currentEpoch--
	}

	dbSnapshot, err := v.state.getNearestOrEpochSnapshot(epochToQuery, dbTx)
	if err != nil {
		return nil, err
	}

	if dbSnapshot != nil {
		// if we do not have any snapshot in memory, or db snapshot is newer than the one in memory
		// return the one from db
		if cachedSnapshot == nil || dbSnapshot.Epoch > cachedSnapshot.Epoch {
			cachedSnapshot = dbSnapshot
			// save it in cache as well, since it doesn't exist
			v.snapshots[dbSnapshot.Epoch] = dbSnapshot.copy()
		}
	}

	return cachedSnapshot, nil
}

// getNextEpochEndingBlock gets the epoch ending block of a newer epoch
// It start checking the blocks from the provided epoch ending block of the previous epoch
func (v *ValidatorsSnapshotCache) getNextEpochEndingBlock(latestEpochEndingBlock uint64) (uint64, error) {
	blockNumber := latestEpochEndingBlock + 1 // get next block

	_, extra, err := helpers.GetBlockData(blockNumber, v.blockchain)
	if err != nil {
		return 0, err
	}

	startEpoch := extra.BlockMetaData.EpochNumber
	epoch := startEpoch

	for startEpoch == epoch {
		blockNumber++

		_, extra, err := helpers.GetBlockData(blockNumber, v.blockchain)
		if err != nil {
			if errors.Is(err, blockchain.ErrNoBlock) {
				return blockNumber - 1, nil
			}

			return 0, err
		}

		epoch = extra.BlockMetaData.EpochNumber
	}

	return blockNumber - 1, nil
}

// isEpochEndingBlock checks if given block is an epoch ending block
func isEpochEndingBlock(blockNumber uint64, extra *polytypes.Extra, blockchain polychain.Blockchain) (bool, error) {
	if extra.Validators == nil {
		// non epoch ending blocks have validator set delta as nil
		return false, nil
	}

	if !extra.Validators.IsEmpty() {
		// if validator set delta is not empty, the validator set was changed in this block
		// meaning the epoch changed as well
		return true, nil
	}

	_, nextBlockExtra, err := helpers.GetBlockData(blockNumber+1, blockchain)
	if err != nil {
		return false, err
	}

	// validator set delta can be empty (no change in validator set happened)
	// so we need to check if their epoch numbers are different
	return extra.BlockMetaData.EpochNumber != nextBlockExtra.BlockMetaData.EpochNumber, nil
}
