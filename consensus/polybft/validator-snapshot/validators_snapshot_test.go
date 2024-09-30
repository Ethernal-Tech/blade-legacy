package validatorsnapshot

import (
	"fmt"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	polytesting "github.com/0xPolygon/polygon-edge/consensus/polybft/testing"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestValidatorsSnapshotCache_GetSnapshot_Build(t *testing.T) {
	t.Parallel()
	assertions := require.New(t)

	const (
		totalValidators  = 10
		validatorSetSize = 5
		epochSize        = uint64(10)
	)

	allValidators := validator.NewTestValidators(t, totalValidators).GetPublicIdentities()

	var oddValidators, evenValidators validator.AccountSet

	for i := 0; i < totalValidators; i++ {
		if i%2 == 0 {
			evenValidators = append(evenValidators, allValidators[i])
		} else {
			oddValidators = append(oddValidators, allValidators[i])
		}
	}

	headersMap := &polytesting.TestHeadersMap{HeadersByNumber: make(map[uint64]*types.Header)}

	createHeaders(t, headersMap, 0, epochSize-1, 1, nil, allValidators[:validatorSetSize])
	createHeaders(t, headersMap, epochSize, 2*epochSize-1, 2, allValidators[:validatorSetSize], allValidators[validatorSetSize:])
	createHeaders(t, headersMap, 2*epochSize, 3*epochSize-1, 3, allValidators[validatorSetSize:], oddValidators)
	createHeaders(t, headersMap, 3*epochSize, 4*epochSize-1, 4, oddValidators, evenValidators)

	var cases = []struct {
		blockNumber       uint64
		expectedSnapshot  validator.AccountSet
		validatorsOverlap bool
		parents           []*types.Header
	}{
		{4, allValidators[:validatorSetSize], false, nil},
		{1 * epochSize, allValidators[validatorSetSize:], false, nil},
		{13, allValidators[validatorSetSize:], false, nil},
		{27, oddValidators, true, nil},
		{36, evenValidators, true, nil},
		{4, allValidators[:validatorSetSize], false, headersMap.GetHeaders()},
		{13, allValidators[validatorSetSize:], false, headersMap.GetHeaders()},
		{27, oddValidators, true, headersMap.GetHeaders()},
		{36, evenValidators, true, headersMap.GetHeaders()},
	}

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)

	cache, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	assertions.NoError(err)

	testValidatorsCache := &testValidatorsCache{
		ValidatorsSnapshotCache: cache,
	}

	for _, c := range cases {
		snapshot, err := testValidatorsCache.GetSnapshot(c.blockNumber, c.parents, nil)

		assertions.NoError(err)
		assertions.Len(snapshot, c.expectedSnapshot.Len())

		if c.validatorsOverlap {
			for _, validator := range c.expectedSnapshot {
				// Order of validators is not preserved, because there are overlapping between validators set.
				// In that case, at the beginning of the set are the ones preserved from the previous validator set.
				// Newly validators are added to the end after the one from previous validator set.
				assertions.True(snapshot.ContainsAddress(validator.Address))
			}
		} else {
			assertions.Equal(c.expectedSnapshot, snapshot)
		}

		assertions.NoError(testValidatorsCache.cleanValidatorsCache())

		if c.parents != nil {
			blockchainMock.AssertNotCalled(t, "GetHeaderByNumber")
		}
	}
}

func TestValidatorsSnapshotCache_GetSnapshot_FetchFromCache(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	const (
		totalValidators  = 10
		validatorSetSize = 5
	)

	allValidators := validator.NewTestValidators(t, totalValidators).GetPublicIdentities()
	epochOneValidators := validator.AccountSet{allValidators[0], allValidators[len(allValidators)-1]}
	epochTwoValidators := allValidators[1 : len(allValidators)-2]

	headersMap := &polytesting.TestHeadersMap{HeadersByNumber: make(map[uint64]*types.Header)}
	createHeaders(t, headersMap, 0, 9, 1, nil, allValidators)
	createHeaders(t, headersMap, 10, 19, 2, allValidators, epochOneValidators)
	createHeaders(t, headersMap, 20, 29, 3, epochOneValidators, epochTwoValidators)

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)

	cache, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	require.NoError(err)

	testValidatorsCache := &testValidatorsCache{
		ValidatorsSnapshotCache: cache,
	}

	require.NoError(testValidatorsCache.StoreSnapshot(&ValidatorSnapshot{1, 10, epochOneValidators}, nil))
	require.NoError(testValidatorsCache.StoreSnapshot(&ValidatorSnapshot{2, 20, epochTwoValidators}, nil))

	// Fetch snapshot from in memory cache
	snapshot, err := testValidatorsCache.GetSnapshot(10, nil, nil)
	require.NoError(err)
	require.Equal(epochOneValidators, snapshot)

	// Invalidate in memory cache
	testValidatorsCache.snapshots = map[uint64]*ValidatorSnapshot{}
	require.NoError(testValidatorsCache.state.removeAllValidatorSnapshots())
	// Fetch snapshot from database
	snapshot, err = testValidatorsCache.GetSnapshot(10, nil, nil)
	require.NoError(err)
	require.Equal(epochOneValidators, snapshot)

	snapshot, err = testValidatorsCache.GetSnapshot(20, nil, nil)
	require.NoError(err)
	require.Equal(epochTwoValidators, snapshot)
}

func TestValidatorsSnapshotCache_Cleanup(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	blockchainMock := new(polychain.BlockchainMock)
	c, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	require.NoError(err)

	cache := &testValidatorsCache{
		ValidatorsSnapshotCache: c,
	}
	snapshot := validator.NewTestValidators(t, 3).GetPublicIdentities()
	maxEpoch := uint64(0)

	for i := uint64(0); i < ValidatorSnapshotLimit; i++ {
		require.NoError(cache.StoreSnapshot(&ValidatorSnapshot{i, i * 10, snapshot}, nil))

		maxEpoch++
	}

	require.NoError(cache.cleanup(nil))

	// assertions for remaining snapshots in the in memory cache
	require.Len(cache.snapshots, NumberOfSnapshotsToLeaveInMemory)

	currentEpoch := maxEpoch

	for i := 0; i < NumberOfSnapshotsToLeaveInMemory; i++ {
		currentEpoch--
		currentSnapshot, snapExists := cache.snapshots[currentEpoch]
		require.True(snapExists, fmt.Sprintf("failed to fetch in memory snapshot for epoch %d", currentEpoch))
		require.Equal(snapshot, currentSnapshot.Snapshot, fmt.Sprintf("snapshots for epoch %d are not equal", currentEpoch))
	}

	stats, err := cache.state.validatorSnapshotsDBStats()
	require.NoError(err)

	// assertions for remaining snapshots in database
	require.Equal(stats.KeyN, NumberOfSnapshotsToLeaveInDB)

	currentEpoch = maxEpoch

	for i := 0; i < NumberOfSnapshotsToLeaveInDB; i++ {
		currentEpoch--
		currentSnapshot, err := cache.state.getValidatorSnapshot(currentEpoch)
		require.NoError(err, fmt.Sprintf("failed to fetch database snapshot for epoch %d", currentEpoch))
		require.Equal(snapshot, currentSnapshot.Snapshot, fmt.Sprintf("snapshots for epoch %d are not equal", currentEpoch))
	}
}

func TestValidatorsSnapshotCache_ComputeSnapshot_UnknownBlock(t *testing.T) {
	t.Parallel()
	assertions := assert.New(t)

	const (
		totalValidators  = 15
		validatorSetSize = totalValidators / 2
		epochSize        = uint64(10)
	)

	allValidators := validator.NewTestValidators(t, totalValidators).GetPublicIdentities()
	headersMap := &polytesting.TestHeadersMap{}
	headersMap.AddHeader(createValidatorDeltaHeader(t, 0, 0, nil, allValidators[:validatorSetSize]))
	headersMap.AddHeader(createValidatorDeltaHeader(t, 1*epochSize, 1, allValidators[:validatorSetSize], allValidators[validatorSetSize:]))

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)

	cache, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	assertions.NoError(err)

	testValidatorsCache := &testValidatorsCache{
		ValidatorsSnapshotCache: cache,
	}

	snapshot, err := testValidatorsCache.computeSnapshot(nil, 5*epochSize, nil)
	assertions.Nil(snapshot)
	assertions.ErrorContains(err, "unknown block. Block number=50")
}

func TestValidatorsSnapshotCache_ComputeSnapshot_IncorrectExtra(t *testing.T) {
	t.Parallel()
	assertions := assert.New(t)

	const (
		totalValidators  = 6
		validatorSetSize = totalValidators / 2
		epochSize        = uint64(10)
	)

	allValidators := validator.NewTestValidators(t, totalValidators).GetPublicIdentities()
	headersMap := &polytesting.TestHeadersMap{}
	invalidHeader := createValidatorDeltaHeader(t, 1*epochSize, 1, allValidators[:validatorSetSize], allValidators[validatorSetSize:])
	invalidHeader.ExtraData = []byte{0x2, 0x7}
	headersMap.AddHeader(invalidHeader)

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)
	cache, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	assertions.NoError(err)

	testValidatorsCache := &testValidatorsCache{
		ValidatorsSnapshotCache: cache,
	}

	snapshot, err := testValidatorsCache.computeSnapshot(nil, 1*epochSize, nil)
	assertions.Nil(snapshot)
	assertions.ErrorContains(err, "failed to decode extra from the block#10: wrong extra size: 2")
}

func TestValidatorsSnapshotCache_ComputeSnapshot_ApplyDeltaFail(t *testing.T) {
	t.Parallel()
	assertions := assert.New(t)

	const (
		totalValidators  = 6
		validatorSetSize = totalValidators / 2
		epochSize        = uint64(10)
	)

	allValidators := validator.NewTestValidators(t, totalValidators).GetPublicIdentities()
	headersMap := &polytesting.TestHeadersMap{}
	headersMap.AddHeader(createValidatorDeltaHeader(t, 0, 0, nil, allValidators[:validatorSetSize]))
	headersMap.AddHeader(createValidatorDeltaHeader(t, 1*epochSize, 1, nil, allValidators[:validatorSetSize]))

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)
	cache, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	assertions.NoError(err)

	testValidatorsCache := &testValidatorsCache{
		ValidatorsSnapshotCache: cache,
	}

	snapshot, err := testValidatorsCache.computeSnapshot(&ValidatorSnapshot{0, 0, allValidators}, 1*epochSize, nil)
	assertions.Nil(snapshot)
	assertions.ErrorContains(err, "failed to apply delta to the validators snapshot, block#10")
}

func TestValidatorsSnapshotCache_Empty(t *testing.T) {
	t.Parallel()

	headersMap := &polytesting.TestHeadersMap{HeadersByNumber: make(map[uint64]*types.Header)}

	createHeaders(t, headersMap, 0, 1, 1, nil, nil)

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)
	cache, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	require.NoError(t, err)

	testValidatorsCache := &testValidatorsCache{
		ValidatorsSnapshotCache: cache,
	}

	_, err = testValidatorsCache.GetSnapshot(1, nil, nil)
	assert.ErrorContains(t, err, "validator snapshot is empty for block")
}

func TestValidatorsSnapshotCache_HugeBuild(t *testing.T) {
	t.Parallel()

	type epochValidatorSetIndexes struct {
		firstValIndex int
		lastValIndex  int
	}

	const (
		epochSize                 = uint64(10)
		lastBlock                 = uint64(100_000)
		numOfEpochsToChangeValSet = 50
		totalValidators           = 20
		validatorSetSize          = 5
	)

	allValidators := validator.NewTestValidators(t, totalValidators).GetPublicIdentities()
	headersMap := &polytesting.TestHeadersMap{HeadersByNumber: make(map[uint64]*types.Header)}

	oldValidators := allValidators[:validatorSetSize]
	newValidators := oldValidators
	firstValIndex := 0
	lastValIndex := validatorSetSize
	epochValidators := map[uint64]epochValidatorSetIndexes{}

	// create headers for the first epoch separately
	createHeaders(t, headersMap, 0, epochSize-1, 1, nil, newValidators)

	for i := epochSize; i < lastBlock; i += epochSize {
		from := i
		to := i + epochSize - 1
		epoch := i/epochSize + 1

		oldValidators = newValidators

		if epoch%numOfEpochsToChangeValSet == 0 {
			// every n epochs, change validators
			firstValIndex = lastValIndex
			lastValIndex += validatorSetSize

			if lastValIndex > totalValidators {
				firstValIndex = 0
				lastValIndex = validatorSetSize
			}

			newValidators = allValidators[firstValIndex:lastValIndex]
		}

		epochValidators[epoch] = epochValidatorSetIndexes{firstValIndex, lastValIndex}

		createHeaders(t, headersMap, from, to, epoch, oldValidators, newValidators)
	}

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(headersMap.GetHeader)

	validatorsSnapshotCache, err := NewValidatorsSnapshotCache(hclog.NewNullLogger(), state.NewTestState(t), blockchainMock)
	require.NoError(t, err)

	s := time.Now().UTC()

	snapshot, err := validatorsSnapshotCache.GetSnapshot(lastBlock-epochSize, nil, nil)

	t.Log("Time needed to calculate snapshot:", time.Since(s))

	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.NotEmpty(t, snapshot)

	// check if the validators of random epochs are as expected
	snapshot, err = validatorsSnapshotCache.GetSnapshot(46, nil, nil) // epoch 5 where validator set did not change
	require.NoError(t, err)

	epochValIndexes, ok := epochValidators[5]
	require.True(t, ok)
	require.True(t, allValidators[epochValIndexes.firstValIndex:epochValIndexes.lastValIndex].Equals(snapshot))

	snapshot, err = validatorsSnapshotCache.GetSnapshot(numOfEpochsToChangeValSet*epochSize, nil, nil) // epoch 50 where validator set was changed
	require.NoError(t, err)

	epochValIndexes, ok = epochValidators[numOfEpochsToChangeValSet]
	require.True(t, ok)
	require.True(t, allValidators[epochValIndexes.firstValIndex:epochValIndexes.lastValIndex].Equals(snapshot))

	snapshot, err = validatorsSnapshotCache.GetSnapshot(2*numOfEpochsToChangeValSet*epochSize, nil, nil) // epoch 100 where validator set was changed
	require.NoError(t, err)

	epochValIndexes, ok = epochValidators[2*numOfEpochsToChangeValSet]
	require.True(t, ok)
	require.True(t, allValidators[epochValIndexes.firstValIndex:epochValIndexes.lastValIndex].Equals(snapshot))

	snapshot, err = validatorsSnapshotCache.GetSnapshot(57903, nil, nil) // epoch 5790 where validator set did not change
	require.NoError(t, err)

	epochValIndexes, ok = epochValidators[57903/epochSize+1]
	require.True(t, ok)
	require.True(t, allValidators[epochValIndexes.firstValIndex:epochValIndexes.lastValIndex].Equals(snapshot))

	snapshot, err = validatorsSnapshotCache.GetSnapshot(99991, nil, nil) // epoch 10000 where validator set did not change
	require.NoError(t, err)

	epochValIndexes, ok = epochValidators[99991/epochSize+1]
	require.True(t, ok)
	require.True(t, allValidators[epochValIndexes.firstValIndex:epochValIndexes.lastValIndex].Equals(snapshot))
}

func TestHelpers_isEpochEndingBlock_DeltaNotEmpty(t *testing.T) {
	t.Parallel()

	validators := validator.NewTestValidators(t, 3).GetPublicIdentities()

	bitmap := bitmap.Bitmap{}
	bitmap.Set(0)

	delta := &validator.ValidatorSetDelta{
		Added:   validators[1:],
		Removed: bitmap,
	}

	extra := &polytypes.Extra{Validators: delta}
	blockNumber := uint64(20)

	isEndOfEpoch, err := isEpochEndingBlock(blockNumber, extra, new(polychain.BlockchainMock))
	require.NoError(t, err)
	require.True(t, isEndOfEpoch)
}

func TestHelpers_isEpochEndingBlock_NoBlock(t *testing.T) {
	t.Parallel()

	blockchainMock := new(polychain.BlockchainMock)
	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(&types.Header{}, false)

	extra := &polytypes.Extra{Validators: &validator.ValidatorSetDelta{}}
	blockNumber := uint64(20)

	isEndOfEpoch, err := isEpochEndingBlock(blockNumber, extra, blockchainMock)
	require.ErrorIs(t, blockchain.ErrNoBlock, err)
	require.False(t, isEndOfEpoch)
}

func TestHelpers_isEpochEndingBlock_EpochsNotTheSame(t *testing.T) {
	t.Parallel()

	blockchainMock := new(polychain.BlockchainMock)

	nextBlockExtra := &polytypes.Extra{Validators: &validator.ValidatorSetDelta{}, BlockMetaData: &polytypes.BlockMetaData{EpochNumber: 3}}
	nextBlock := &types.Header{
		Number:    21,
		ExtraData: nextBlockExtra.MarshalRLPTo(nil),
	}

	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(nextBlock, true)

	extra := &polytypes.Extra{Validators: &validator.ValidatorSetDelta{}, BlockMetaData: &polytypes.BlockMetaData{EpochNumber: 2}}
	blockNumber := uint64(20)

	isEndOfEpoch, err := isEpochEndingBlock(blockNumber, extra, blockchainMock)
	require.NoError(t, err)
	require.True(t, isEndOfEpoch)
}

func TestHelpers_isEpochEndingBlock_EpochsAreTheSame(t *testing.T) {
	t.Parallel()

	blockchainMock := new(polychain.BlockchainMock)

	nextBlockExtra := &polytypes.Extra{Validators: &validator.ValidatorSetDelta{}, BlockMetaData: &polytypes.BlockMetaData{EpochNumber: 2}}
	nextBlock := &types.Header{
		Number:    16,
		ExtraData: nextBlockExtra.MarshalRLPTo(nil),
	}

	blockchainMock.On("GetHeaderByNumber", mock.Anything).Return(nextBlock, true)

	extra := &polytypes.Extra{Validators: &validator.ValidatorSetDelta{}, BlockMetaData: &polytypes.BlockMetaData{EpochNumber: 2}}
	blockNumber := uint64(15)

	isEndOfEpoch, err := isEpochEndingBlock(blockNumber, extra, blockchainMock)
	require.NoError(t, err)
	require.False(t, isEndOfEpoch)
}

func createHeaders(t *testing.T, headersMap *polytesting.TestHeadersMap,
	fromBlock, toBlock, epoch uint64, oldValidators, newValidators validator.AccountSet) {
	t.Helper()

	headersMap.AddHeader(createValidatorDeltaHeader(t, fromBlock, epoch-1, oldValidators, newValidators))

	for i := fromBlock + 1; i <= toBlock; i++ {
		headersMap.AddHeader(createValidatorDeltaHeader(t, i, epoch, nil, nil))
	}
}

func createValidatorDeltaHeader(t *testing.T, blockNumber, epoch uint64, oldValidatorSet, newValidatorSet validator.AccountSet) *types.Header {
	t.Helper()

	delta, _ := validator.CreateValidatorSetDelta(oldValidatorSet, newValidatorSet)
	extra := &polytypes.Extra{Validators: delta, BlockMetaData: &polytypes.BlockMetaData{EpochNumber: epoch}}

	return &types.Header{
		Number:    blockNumber,
		ExtraData: extra.MarshalRLPTo(nil),
	}
}

type testValidatorsCache struct {
	*ValidatorsSnapshotCache
}

func (c *testValidatorsCache) cleanValidatorsCache() error {
	c.snapshots = make(map[uint64]*ValidatorSnapshot)

	return c.state.removeAllValidatorSnapshots()
}
