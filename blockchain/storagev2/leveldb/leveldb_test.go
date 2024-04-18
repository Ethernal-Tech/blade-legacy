package leveldb

import (
	"context"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/blockchain/storagev2"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func newStorage(t *testing.T) (*storagev2.Storage, func(), string) {
	t.Helper()

	path, err := os.MkdirTemp("", "leveldbV2")
	if err != nil {
		t.Fatal(err)
	}

	s, err := NewLevelDBStorage(path, hclog.NewNullLogger())
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

func countLdbFilesInPath(path string) int {
	pattern := filepath.Join(path, "*.ldb")

	files, err := filepath.Glob(pattern)
	if err != nil {
		return -1
	}

	return len(files)
}

func dbSize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info != nil && !info.IsDir() && strings.Contains(info.Name(), ".ldb") {
			size += info.Size()
		}

		return err
	})

	return size, err
}

func writeBlock(t *testing.T, s *storagev2.Storage, b *types.FullBlock) {
	t.Helper()

	batchWriter := s.NewWriter()

	batchWriter.PutBody(b.Block.Number(), b.Block.Hash(), b.Block.Body())

	for _, tx := range b.Block.Transactions {
		batchWriter.PutTxLookup(tx.Hash(), b.Block.Number())
	}

	batchWriter.PutHeadHash(b.Block.Header.Hash)
	batchWriter.PutHeadNumber(b.Block.Number())
	batchWriter.PutBlockLookup(b.Block.Hash(), b.Block.Number())
	batchWriter.PutHeader(b.Block.Header)
	batchWriter.PutReceipts(b.Block.Number(), b.Block.Hash(), b.Receipts)
	batchWriter.PutCanonicalHash(b.Block.Number(), b.Block.Hash())
	require.NoError(t, batchWriter.WriteBatch())
}

func readBlock(t *testing.T, s *storagev2.Storage, blockCount int, wg *sync.WaitGroup, ctx context.Context) {
	t.Helper()

	defer wg.Done()

	ticker := time.NewTicker(20 * time.Millisecond)

	readCount := 1000
	for i := 1; i <= readCount; i++ {
		n := uint64(1 + rand.Intn(blockCount))

		hn, ok := s.ReadHeadNumber()
		if ok && n <= hn {
			// If head number is read and chain progresed enough to contain canonical block #n
			h, ok := s.ReadCanonicalHash(n)
			require.True(t, ok)

			_, err := s.ReadBody(n, h)
			require.NoError(t, err)

			_, err = s.ReadHeader(n, h)
			require.NoError(t, err)

			_, err = s.ReadReceipts(n, h)
			require.NoError(t, err)

			b, err := s.ReadBlockLookup(h)
			require.NoError(t, err)

			require.Equal(t, n, b)
		}

		select {
		case <-ctx.Done():
			ticker.Stop()

			return
		case <-ticker.C:
		}
	}

	t.Logf("\tRead thread finished")
}

func TestStorage(t *testing.T) {
	storagev2.TestStorage(t, newStorage)
}

func TestWriteReadFullBlockInParallel(t *testing.T) {
	s, cleanUpFn, path := newStorage(t)
	defer func() {
		s.Close()
		cleanUpFn()
	}()

	var wg sync.WaitGroup

	blockCount := 100
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)

	signchan := make(chan os.Signal, 1)
	signal.Notify(signchan, syscall.SIGINT)

	go func() {
		<-signchan
		cancel()
	}()

	blockchain := make(chan *types.FullBlock, 1)
	go storagev2.GenerateBlocks(t, blockCount, blockchain, ctx)

	readThreads := 3
	for i := 1; i <= readThreads; i++ {
		wg.Add(1)

		go readBlock(t, s, blockCount, &wg, ctx)
	}

insertloop:
	for i := 1; i <= blockCount; i++ {
		select {
		case <-ctx.Done():
			break insertloop
		case b := <-blockchain:
			writeBlock(t, s, b)
			t.Logf("writing block %d", i)
		}
	}

	size, err := dbSize(path)
	require.NoError(t, err)
	t.Logf("\tldb file count: %d", countLdbFilesInPath(path))
	t.Logf("\tdb size %d MBs", size/(1*opt.MiB))
	wg.Wait()
}
