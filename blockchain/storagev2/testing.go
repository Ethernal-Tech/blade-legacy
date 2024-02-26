package storagev2

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type PlaceholderStorage func(t *testing.T) (*Storage, func())

var (
	addr1 = types.StringToAddress("1")
	addr2 = types.StringToAddress("2")

	hash1 = types.StringToHash("1")
	hash2 = types.StringToHash("2")
)

// TestStorage tests a set of tests on a storage
func TestStorage(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	t.Run("testCanonicalChain", func(t *testing.T) {
		testCanonicalChain(t, m)
	})
	t.Run("testDifficulty", func(t *testing.T) {
		testDifficulty(t, m)
	})
	t.Run("testHead", func(t *testing.T) {
		testHead(t, m)
	})
	t.Run("testForks", func(t *testing.T) {
		testForks(t, m)
	})
	t.Run("testHeader", func(t *testing.T) {
		testHeader(t, m)
	})
	t.Run("testBody", func(t *testing.T) {
		testBody(t, m)
	})
	t.Run("testWriteCanonicalHeader", func(t *testing.T) {
		testWriteCanonicalHeader(t, m)
	})
	t.Run("testReceipts", func(t *testing.T) {
		testReceipts(t, m)
	})
}

func testCanonicalChain(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	var cases = []struct {
		Number     uint64
		ParentHash types.Hash
		Hash       types.Hash
	}{
		{
			Number:     1,
			ParentHash: types.StringToHash("111"),
		},
		{
			Number:     1,
			ParentHash: types.StringToHash("222"),
		},
		{
			Number:     2,
			ParentHash: types.StringToHash("111"),
		},
	}

	for _, cc := range cases {
		batch := s.NewWriter()

		h := &types.Header{
			Number:     cc.Number,
			ParentHash: cc.ParentHash,
			ExtraData:  []byte{0x1},
		}

		hash := h.Hash

		batch.PutHeader(h)
		batch.PutCanonicalHash(cc.Number, hash)

		require.NoError(t, batch.WriteBatch())

		data, ok := s.ReadCanonicalHash(cc.Number)
		assert.True(t, ok)

		if !reflect.DeepEqual(data, hash) {
			t.Fatal("not match")
		}
	}
}

func testDifficulty(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	var cases = []struct {
		Diff *big.Int
	}{
		{
			Diff: big.NewInt(10),
		},
		{
			Diff: big.NewInt(11),
		},
		{
			Diff: big.NewInt(12),
		},
	}

	for indx, cc := range cases {
		batch := s.NewWriter()

		h := &types.Header{
			Number:    uint64(indx),
			ExtraData: []byte{},
		}

		batch.PutHeader(h)
		batch.PutBlockLookup(h.Hash, h.Number)
		batch.PutTotalDifficulty(h.Number, cc.Diff)

		require.NoError(t, batch.WriteBatch())

		diff, ok := s.ReadTotalDifficulty(h.Number)
		assert.True(t, ok)

		if !reflect.DeepEqual(cc.Diff, diff) {
			t.Fatal("bad")
		}
	}
}

func testHead(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	for i := uint64(0); i < 5; i++ {
		batch := s.NewWriter()

		h := &types.Header{
			Number:    i,
			ExtraData: []byte{},
		}
		hash := h.Hash

		batch.PutHeader(h)
		batch.PutHeadNumber(i)
		batch.PutHeadHash(hash)

		require.NoError(t, batch.WriteBatch())

		n2, ok := s.ReadHeadNumber()
		assert.True(t, ok)

		if n2 != i {
			t.Fatal("bad")
		}

		hash1, ok := s.ReadHeadHash()
		assert.True(t, ok)

		if !reflect.DeepEqual(hash1, hash) {
			t.Fatal("bad")
		}
	}
}

func testForks(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	var cases = []struct {
		Forks []types.Hash
	}{
		{[]types.Hash{types.StringToHash("111"), types.StringToHash("222")}},
		{[]types.Hash{types.StringToHash("111")}},
	}

	for _, cc := range cases {
		batch := s.NewWriter()

		batch.PutForks(cc.Forks)

		require.NoError(t, batch.WriteBatch())

		forks, err := s.ReadForks()
		assert.NoError(t, err)

		if !reflect.DeepEqual(cc.Forks, forks) {
			t.Fatal("bad")
		}
	}
}

func testHeader(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	header := &types.Header{
		Number:     5,
		Difficulty: 17179869184,
		ParentHash: types.StringToHash("11"),
		Timestamp:  10,
		// if not set it will fail
		ExtraData: hex.MustDecodeHex("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa"),
	}
	header.ComputeHash()

	batch := s.NewWriter()

	batch.PutHeader(header)

	require.NoError(t, batch.WriteBatch())

	header1, err := s.ReadHeader(header.Number)
	assert.NoError(t, err)

	if !reflect.DeepEqual(header, header1) {
		t.Fatal("bad")
	}
}

func testBody(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	header := &types.Header{
		Number:     5,
		Difficulty: 10,
		ParentHash: types.StringToHash("11"),
		Timestamp:  10,
		ExtraData:  []byte{}, // if not set it will fail
	}

	batch := s.NewWriter()

	batch.PutHeader(header)

	require.NoError(t, batch.WriteBatch())

	addr1 := types.StringToAddress("11")
	t0 := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &addr1,
		Value:    big.NewInt(1),
		Gas:      11,
		GasPrice: big.NewInt(11),
		Input:    []byte{1, 2},
		V:        big.NewInt(1),
	})
	t0.ComputeHash()

	addr2 := types.StringToAddress("22")
	t1 := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &addr2,
		Value:    big.NewInt(1),
		Gas:      22,
		GasPrice: big.NewInt(11),
		Input:    []byte{4, 5},
		V:        big.NewInt(2),
	})
	t1.ComputeHash()

	block := types.Block{
		Header:       header,
		Transactions: []*types.Transaction{t0, t1},
	}

	batch2 := s.NewWriter()
	body0 := block.Body()

	batch2.PutBody(header.Number, body0)

	require.NoError(t, batch2.WriteBatch())

	body1, err := s.ReadBody(header.Number)
	assert.NoError(t, err)

	// NOTE: reflect.DeepEqual does not seem to work, check the hash of the transactions
	tx0, tx1 := body0.Transactions, body1.Transactions
	if len(tx0) != len(tx1) {
		t.Fatal("lengths are different")
	}

	for indx, i := range tx0 {
		if i.Hash() != tx1[indx].Hash() {
			t.Fatal("tx not correct")
		}
	}
}

func testReceipts(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	batch := s.NewWriter()

	h := &types.Header{
		Difficulty: 133,
		Number:     11,
		ExtraData:  []byte{},
	}
	h.ComputeHash()

	body := &types.Body{
		Transactions: []*types.Transaction{
			types.NewTx(&types.StateTx{
				Nonce:    1000,
				Gas:      50,
				GasPrice: new(big.Int).SetUint64(100),
				V:        big.NewInt(11),
			}),
		},
	}
	receipts := []*types.Receipt{
		{
			Root:              types.StringToHash("1"),
			CumulativeGasUsed: 10,
			TxHash:            body.Transactions[0].Hash(),
			LogsBloom:         types.Bloom{0x1},
			Logs: []*types.Log{
				{
					Address: addr1,
					Topics:  []types.Hash{hash1, hash2},
					Data:    []byte{0x1, 0x2},
				},
				{
					Address: addr2,
					Topics:  []types.Hash{hash1},
				},
			},
		},
		{
			Root:              types.StringToHash("1"),
			CumulativeGasUsed: 10,
			TxHash:            body.Transactions[0].Hash(),
			LogsBloom:         types.Bloom{0x1},
			GasUsed:           10,
			ContractAddress:   &types.Address{0x1},
			Logs: []*types.Log{
				{
					Address: addr2,
					Topics:  []types.Hash{hash1},
				},
			},
		},
	}

	batch.PutHeader(h)
	batch.PutBody(h.Number, body)
	batch.PutReceipts(h.Number, receipts)

	require.NoError(t, batch.WriteBatch())

	found, err := s.ReadReceipts(h.Number)
	assert.NoError(t, err)
	assert.True(t, reflect.DeepEqual(receipts, found))
}

func testWriteCanonicalHeader(t *testing.T, m PlaceholderStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	h := &types.Header{
		Number:    100,
		ExtraData: []byte{0x1},
	}
	h.ComputeHash()

	diff := new(big.Int).SetUint64(100)
	batch := s.NewWriter()

	batch.PutCanonicalHeader(h, diff)

	require.NoError(t, batch.WriteBatch())

	hh, err := s.ReadHeader(h.Number)
	assert.NoError(t, err)

	if !reflect.DeepEqual(h, hh) {
		t.Fatal("bad header")
	}

	headHash, ok := s.ReadHeadHash()
	assert.True(t, ok)

	if headHash != h.Hash {
		t.Fatal("head hash not correct")
	}

	headNum, ok := s.ReadHeadNumber()
	assert.True(t, ok)

	if headNum != h.Number {
		t.Fatal("head num not correct")
	}

	canHash, ok := s.ReadCanonicalHash(h.Number)
	assert.True(t, ok)

	if canHash != h.Hash {
		t.Fatal("canonical hash not correct")
	}
}

// Storage delegators

type readCanonicalHashDelegate func(uint64) (types.Hash, error)
type readHeadHashDelegate func() (types.Hash, error)
type readHeadNumberDelegate func() (uint64, error)
type readForksDelegate func() ([]types.Hash, error)
type readTotalDifficultyDelegate func(uint64) (*big.Int, error)
type readHeaderDelegate func(uint64) (*types.Header, error)
type readBodyDelegate func(uint64) (*types.Body, error)
type readReceiptsDelegate func(uint64) ([]*types.Receipt, error)
type readTxLookupDelegate func(types.Hash) (uint64, error)
type closeDelegate func() error
type newBatchDelegate func() Batch

type MockStorage struct {
	readCanonicalHashFn   readCanonicalHashDelegate
	readHeadHashFn        readHeadHashDelegate
	readHeadNumberFn      readHeadNumberDelegate
	readForksFn           readForksDelegate
	readTotalDifficultyFn readTotalDifficultyDelegate
	readHeaderFn          readHeaderDelegate
	readBodyFn            readBodyDelegate
	readReceiptsFn        readReceiptsDelegate
	readTxLookupFn        readTxLookupDelegate
	closeFn               closeDelegate
	newBatchFn            newBatchDelegate
}

func NewMockStorage() *MockStorage {
	return &MockStorage{}
}

func (m *MockStorage) ReadCanonicalHash(n uint64) (types.Hash, error) {
	if m.readCanonicalHashFn != nil {
		return m.readCanonicalHashFn(n)
	}

	return types.Hash{}, nil
}

func (m *MockStorage) HookReadCanonicalHash(fn readCanonicalHashDelegate) {
	m.readCanonicalHashFn = fn
}

func (m *MockStorage) ReadHeadHash() (types.Hash, error) {
	if m.readHeadHashFn != nil {
		return m.readHeadHashFn()
	}

	return types.Hash{}, nil
}

func (m *MockStorage) HookReadHeadHash(fn readHeadHashDelegate) {
	m.readHeadHashFn = fn
}

func (m *MockStorage) ReadHeadNumber() (uint64, error) {
	if m.readHeadNumberFn != nil {
		return m.readHeadNumberFn()
	}

	return 0, nil
}

func (m *MockStorage) HookReadHeadNumber(fn readHeadNumberDelegate) {
	m.readHeadNumberFn = fn
}

func (m *MockStorage) ReadForks() ([]types.Hash, error) {
	if m.readForksFn != nil {
		return m.readForksFn()
	}

	return []types.Hash{}, nil
}

func (m *MockStorage) HookReadForks(fn readForksDelegate) {
	m.readForksFn = fn
}

func (m *MockStorage) ReadTotalDifficulty(bn uint64) (*big.Int, error) {
	if m.readTotalDifficultyFn != nil {
		return m.readTotalDifficultyFn(bn)
	}

	return big.NewInt(0), nil
}

func (m *MockStorage) HookReadTotalDifficulty(fn readTotalDifficultyDelegate) {
	m.readTotalDifficultyFn = fn
}

func (m *MockStorage) ReadHeader(bn uint64) (*types.Header, error) {
	if m.readHeaderFn != nil {
		return m.readHeaderFn(bn)
	}

	return &types.Header{}, nil
}

func (m *MockStorage) HookReadHeader(fn readHeaderDelegate) {
	m.readHeaderFn = fn
}

func (m *MockStorage) ReadBody(bn uint64) (*types.Body, error) {
	if m.readBodyFn != nil {
		return m.readBodyFn(bn)
	}

	return &types.Body{}, nil
}

func (m *MockStorage) HookReadBody(fn readBodyDelegate) {
	m.readBodyFn = fn
}

func (m *MockStorage) ReadReceipts(bn uint64) ([]*types.Receipt, error) {
	if m.readReceiptsFn != nil {
		return m.readReceiptsFn(bn)
	}

	return []*types.Receipt{}, nil
}

func (m *MockStorage) HookReadReceipts(fn readReceiptsDelegate) {
	m.readReceiptsFn = fn
}

func (m *MockStorage) ReadTxLookup(hash types.Hash) (uint64, error) {
	if m.readTxLookupFn != nil {
		return m.readTxLookupFn(hash)
	}

	return 0, nil
}

func (m *MockStorage) HookReadTxLookup(fn readTxLookupDelegate) {
	m.readTxLookupFn = fn
}

func (m *MockStorage) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}

	return nil
}

func (m *MockStorage) HookClose(fn closeDelegate) {
	m.closeFn = fn
}

func (m *MockStorage) HookNewBatch(fn newBatchDelegate) {
	m.newBatchFn = fn
}

func (m *MockStorage) NewBatch() Batch {
	if m.newBatchFn != nil {
		return m.newBatchFn()
	}

	return nil
}