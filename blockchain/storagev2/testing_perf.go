package storagev2

import (
	cryptoRand "crypto/rand"
	"math/big"
	mathRand "math/rand"
	"testing"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

const letterBytes = "abcdef0123456789"

func randStringBytes(b *testing.B, n int) string {
	b.Helper()

	retVal := make([]byte, n)
	_, err := cryptoRand.Reader.Read(retVal)
	require.NoError(b, err)

	return string(retVal)
}

func createTxs(b *testing.B, startNonce, count int, from types.Address, to *types.Address) []*types.Transaction {
	b.Helper()

	txs := make([]*types.Transaction, count)

	for i := range txs {
		tx := types.NewTx(types.NewDynamicFeeTx(
			types.WithGas(types.StateTransactionGasLimit),
			types.WithNonce(uint64(startNonce+i)),
			types.WithFrom(from),
			types.WithTo(to),
			types.WithValue(big.NewInt(2000)),
			types.WithGasFeeCap(big.NewInt(100)),
			types.WithGasTipCap(big.NewInt(10)),
		))

		txs[i] = tx
	}

	return txs
}

func createBlock(b *testing.B) *types.FullBlock {
	b.Helper()

	transactionsCount := 2500
	status := types.ReceiptSuccess
	addr1 := types.StringToAddress("17878aa")
	addr2 := types.StringToAddress("2bf5653")
	fb := &types.FullBlock{
		Block: &types.Block{
			Header: &types.Header{
				Number:    0,
				ExtraData: make([]byte, 32),
				Hash:      types.ZeroHash,
			},
			Transactions: createTxs(b, 0, transactionsCount, addr1, &addr2),
			// Uncles:       blockchain.NewTestHeaders(10),
		},
		Receipts: make([]*types.Receipt, transactionsCount),
	}

	logs := make([]*types.Log, 10)

	for i := 0; i < 10; i++ {
		logs[i] = &types.Log{
			Address: addr1,
			Topics:  []types.Hash{types.StringToHash("t1"), types.StringToHash("t2"), types.StringToHash("t3")},
			Data:    []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xbb, 0xaa, 0x01, 0x012},
		}
	}

	for i := 0; i < len(fb.Block.Transactions); i++ {
		fb.Receipts[i] = &types.Receipt{
			TxHash:            fb.Block.Transactions[i].Hash(),
			Root:              types.StringToHash("mockhashstring"),
			TransactionType:   types.LegacyTxType,
			GasUsed:           uint64(100000),
			Status:            &status,
			Logs:              logs,
			CumulativeGasUsed: uint64(100000),
			ContractAddress:   &types.Address{0xaa, 0xbb, 0xcc, 0xdd, 0xab, 0xac},
		}
	}

	for i := 0; i < 5; i++ {
		fb.Receipts[i].LogsBloom = types.CreateBloom(fb.Receipts)
	}

	return fb
}

func updateBlock(b *testing.B, num uint64, fb *types.FullBlock) *types.FullBlock {
	b.Helper()

	var addr types.Address

	fb.Block.Header.Number = num
	fb.Block.Header.ParentHash = types.StringToHash(randStringBytes(b, 12))

	for i := range fb.Block.Transactions {
		addr = types.StringToAddress(randStringBytes(b, 8))
		fb.Block.Transactions[i].SetTo(&addr)
		fb.Block.Transactions[i].ComputeHash()
		fb.Receipts[i].TxHash = fb.Block.Transactions[i].Hash()
	}

	fb.Block.Header.ComputeHash()

	return fb
}

func prepareBatch(b *testing.B, s *Storage, fb *types.FullBlock) *Writer {
	b.Helper()

	batchWriter := s.NewWriter()

	// Lookup 'sorted'
	batchWriter.PutHeadHash(fb.Block.Header.Hash)
	batchWriter.PutHeadNumber(fb.Block.Number())
	batchWriter.PutBlockLookup(fb.Block.Hash(), fb.Block.Number())

	for _, tx := range fb.Block.Transactions {
		batchWriter.PutTxLookup(tx.Hash(), fb.Block.Number())
	}

	// Main DB sorted
	batchWriter.PutBody(fb.Block.Number(), fb.Block.Hash(), fb.Block.Body())
	batchWriter.PutCanonicalHash(fb.Block.Number(), fb.Block.Hash())
	batchWriter.PutHeader(fb.Block.Header)
	batchWriter.PutReceipts(fb.Block.Number(), fb.Block.Hash(), fb.Receipts)

	return batchWriter
}

func testWriteBlockPerf(b *testing.B, blockCount int, s *Storage, time float64) {
	b.Helper()
	fb := createBlock(b)

	for i := 1; i <= blockCount; i++ {
		updateBlock(b, uint64(i), fb)

		b.StartTimer()
		batchWriter := prepareBatch(b, s, fb)
		require.NoError(b, batchWriter.WriteBatch())
		b.StopTimer()
	}

	b.Logf("\ttotal write time %f s", b.Elapsed().Seconds())
	require.LessOrEqual(b, b.Elapsed().Seconds(), time)
}

func testReadBlockPerf(b *testing.B, blockCount int, s *Storage, time float64) {
	b.Helper()

	for i := 1; i <= blockCount; i++ {
		n := uint64(1 + mathRand.Intn(blockCount)) //nolint:gosec

		b.StartTimer()

		h, ok := s.ReadCanonicalHash(n)
		_, err1 := s.ReadBody(n, h)
		_, err3 := s.ReadHeader(n, h)
		_, err4 := s.ReadReceipts(n, h)
		bn, err5 := s.ReadBlockLookup(h)

		b.StopTimer()

		if !ok || err1 != nil || err3 != nil || err4 != nil || err5 != nil {
			b.Logf("\terror")
		}

		require.Equal(b, n, bn)
	}

	b.Logf("\ttotal read time %f s", b.Elapsed().Seconds())
	require.LessOrEqual(b, b.Elapsed().Seconds(), time)
}

func BenchmarkStorage(t *testing.B, blockCount int, s *Storage, writeTime float64, readTime float64) {
	t.Helper()

	t.Run("testWriteBlockPerf", func(t *testing.B) {
		testWriteBlockPerf(t, blockCount, s, writeTime)
	})
	t.Run("testReadBlockPerf", func(t *testing.B) {
		testReadBlockPerf(t, blockCount, s, readTime)
	})
}
