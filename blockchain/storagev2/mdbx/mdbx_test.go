package mdbx

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/blockchain/storagev2"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func newStorage(t *testing.T) (*storagev2.Storage, func(), string) {
	t.Helper()

	path, err := os.MkdirTemp("/tmp", "mdbx")
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

	return s, closeFn, path
}

func dbSize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info != nil && !info.IsDir() && strings.Contains(info.Name(), ".dat") {
			size += info.Size()
		}

		return nil
	})

	return size, err
}

func TestStorage(t *testing.T) {
	storagev2.TestStorage(t, newStorage)
}

func TestWriteFullBlock(t *testing.T) {
	s, cleanUpFn, path := newStorage(t)
	defer func() {
		s.Close()
		cleanUpFn()
	}()

	count := 100
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*45)

	signchan := make(chan os.Signal, 1)
	signal.Notify(signchan, syscall.SIGINT)

	go func() {
		<-signchan
		cancel()
	}()

	blockchain := make(chan *types.FullBlock, 1)
	go storagev2.GenerateBlocks(t, count, blockchain, ctx)

insertloop:
	for i := 1; i <= count; i++ {
		select {
		case <-ctx.Done():
			break insertloop
		case b := <-blockchain:
			batchWriter := s.NewWriter()

			batchWriter.PutBody(b.Block.Number(), b.Block.Hash(), b.Block.Body())

			for _, tx := range b.Block.Transactions {
				batchWriter.PutTxLookup(tx.Hash(), b.Block.Number())
			}

			batchWriter.PutHeader(b.Block.Header)
			batchWriter.PutHeadNumber(uint64(i))
			batchWriter.PutHeadHash(b.Block.Header.Hash)
			batchWriter.PutReceipts(b.Block.Number(), b.Block.Hash(), b.Receipts)
			batchWriter.PutCanonicalHash(uint64(i), b.Block.Hash())
			require.NoError(t, batchWriter.WriteBatch())

			t.Logf("writing block %d", i)
		}
	}

	size, err := dbSize(path)
	require.NoError(t, err)
	t.Logf("\tdb size %d MBs", size/1048576)
}
