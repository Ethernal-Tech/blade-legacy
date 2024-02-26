package mdbx

import (
	"context"
	"crypto/rand"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/blockchain"
	"github.com/0xPolygon/polygon-edge/blockchain/storagev2"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func newStorage(t *testing.T) (*storagev2.Storage, func()) {
	t.Helper()

	path, err := os.MkdirTemp("/tmp", "minimal_storage")
	if err != nil {
		t.Fatal(err)
	}

	s, err := NewMdbxStorage(path, hclog.NewNullLogger())
	if err != nil {
		t.Fatal(err)
	}

	closeFn := func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}

		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}

	return s, closeFn
}

func TestStorage(t *testing.T) {
	storagev2.TestStorage(t, newStorage)
}

func generateTxs(t *testing.T, startNonce, count int, from types.Address, to *types.Address) []*types.Transaction {
	t.Helper()

	txs := make([]*types.Transaction, count)

	for i := range txs {
		tx := types.NewTx(&types.DynamicFeeTx{
			Gas:       types.StateTransactionGasLimit,
			Nonce:     uint64(startNonce + i),
			From:      from,
			To:        to,
			Value:     big.NewInt(2000),
			GasFeeCap: big.NewInt(100),
			GasTipCap: big.NewInt(10),
		})

		input := make([]byte, 1000)
		_, err := rand.Read(input)

		require.NoError(t, err)

		tx.ComputeHash()

		txs[i] = tx
	}

	return txs
}

func generateBlock(t *testing.T, num uint64) *types.FullBlock {
	t.Helper()

	transactionsCount := 2500
	status := types.ReceiptSuccess
	addr1 := types.StringToAddress("17878aa")
	addr2 := types.StringToAddress("2bf5653")
	b := &types.FullBlock{
		Block: &types.Block{
			Header: &types.Header{
				Number:    num,
				ExtraData: make([]byte, 32),
				Hash:      types.ZeroHash,
			},
			Transactions: generateTxs(t, 0, transactionsCount, addr1, &addr2),
			Uncles:       blockchain.NewTestHeaders(10),
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

	for i := 0; i < len(b.Block.Transactions); i++ {
		b.Receipts[i] = &types.Receipt{
			TxHash:            b.Block.Transactions[i].Hash(),
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
		b.Receipts[i].LogsBloom = types.CreateBloom(b.Receipts)
	}

	return b
}

func newStorageP(t *testing.T) (*storagev2.Storage, func(), string) {
	t.Helper()

	p, err := os.MkdirTemp("", "mdbx-test")
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(p, 0755))

	s, err := NewMdbxStorage(p, hclog.NewNullLogger())
	require.NoError(t, err)

	closeFn := func() {
		require.NoError(t, s.Close())

		if err := s.Close(); err != nil {
			t.Fatal(err)
		}

		require.NoError(t, os.RemoveAll(p))
	}

	return s, closeFn, p
}

func generateBlocks(t *testing.T, count int, ch chan *types.FullBlock, ctx context.Context) {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)

	for i := 1; i <= count; i++ {
		b := generateBlock(t, uint64(i))
		select {
		case <-ctx.Done():
			close(ch)
			ticker.Stop()

			return
		case <-ticker.C:
			ch <- b
		}
	}
}

func dbSize(t *testing.T, path string) int64 {
	t.Helper()

	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fail()
		}

		if info != nil && !info.IsDir() && strings.Contains(info.Name(), ".dat") {
			size += info.Size()
		}

		return err
	})
	if err != nil {
		t.Log(err)
	}

	return size
}

func TestWriteFullBlock(t *testing.T) {
	s, _, path := newStorageP(t)
	defer s.Close()

	count := 100
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*45)

	signchan := make(chan os.Signal, 1)
	signal.Notify(signchan, syscall.SIGINT)

	go func() {
		<-signchan
		cancel()
	}()

	blockchain := make(chan *types.FullBlock, 1)
	go generateBlocks(t, count, blockchain, ctx)

insertloop:
	for i := 1; i <= count; i++ {
		select {
		case <-ctx.Done():
			break insertloop
		case b := <-blockchain:
			batchWriter := s.NewWriter()

			batchWriter.PutBody(b.Block.Number(), b.Block.Body())

			for _, tx := range b.Block.Transactions {
				batchWriter.PutTxLookup(tx.Hash(), b.Block.Number())
			}

			batchWriter.PutHeader(b.Block.Header)
			batchWriter.PutHeadNumber(uint64(i))
			batchWriter.PutHeadHash(b.Block.Header.Hash)
			batchWriter.PutReceipts(b.Block.Number(), b.Receipts)
			batchWriter.PutCanonicalHash(uint64(i), b.Block.Hash())

			if err := batchWriter.WriteBatch(); err != nil {
				require.NoError(t, err)
			}

			t.Logf("writing block %d", i)

			size := dbSize(t, path)
			t.Logf("\tdir size %d MBs", size/1_000_000)
		}
	}
}