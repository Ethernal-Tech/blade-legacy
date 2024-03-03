package tests

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	benchmarksDir = "tests/evm-benchmarks/benchmarks"
)

func BenchmarkEVM(b *testing.B) {
	folders, err := listFolders([]string{benchmarksDir})
	require.NoError(b, err)

	for _, folder := range folders {
		files, err := listFiles(folder, ".json")
		require.NoError(b, err)

		for _, file := range files {
			name := getTestName(file)

			b.Run(name, func(b *testing.B) {
				data, err := os.ReadFile(file)
				require.NoError(b, err)

				var testCases map[string]testCase
				if err = json.Unmarshal(data, &testCases); err != nil {
					b.Fatalf("failed to unmarshal %s: %v", file, err)
				}

				for _, tc := range testCases {
					for fork, postState := range tc.Post {
						forks, exists := Forks[fork]
						if !exists {
							b.Logf("%s fork is not supported, skipping test case.", fork)
							continue
						}

						fc := &forkConfig{name: fork, forks: forks}

						for idx, postStateEntry := range postState {
							runBenchmarkTest(b, file, tc, fc, idx, postStateEntry)
						}
					}
				}
			})
		}
	}
}

func runBenchmarkTest(b *testing.B, file string, c testCase, fc *forkConfig, index int, p postEntry) {

}
