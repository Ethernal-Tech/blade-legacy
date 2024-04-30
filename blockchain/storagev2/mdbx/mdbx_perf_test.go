package mdbx

import (
	"os"
	"testing"

	"github.com/0xPolygon/polygon-edge/blockchain/storagev2"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func openStorage(b *testing.B, p string) (*storagev2.Storage, func(), string) {
	b.Helper()

	s, err := NewMdbxStorage(p, hclog.NewNullLogger())
	require.NoError(b, err)

	closeFn := func() {
		require.NoError(b, s.Close())

		if err := s.Close(); err != nil {
			b.Fatal(err)
		}

		require.NoError(b, os.RemoveAll(p))
	}

	return s, closeFn, p
}

func Benchmark(b *testing.B) {
	b.StopTimer()

	s, cleanUpFn, path := openStorage(b, "/tmp/mdbx-test-perf")
	defer func() {
		s.Close()
		cleanUpFn()
	}()

	blockCount := 1000
	storagev2.BenchmarkStorage(b, blockCount, s, 40, 18) // CI times

	size, err := dbSize(path)
	require.NoError(b, err)
	b.Logf("\tdb size %d MB", size/1048576)
}
