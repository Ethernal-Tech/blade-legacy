package tests

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

// Currently used test cases suite version is v10.4.
// It does not include Merge hardfork test cases.

const (
	stateTests         = "tests/GeneralStateTests"
	testGenesisBaseFee = 0x0a
)

var (
	ripemd = types.StringToAddress("0000000000000000000000000000000000000003")
)

func RunSpecificTest(t *testing.T, file string, c testCase, name, fork string, index int, p postEntry) {
	t.Helper()

	config, ok := Forks[fork]
	if !ok {
		t.Logf("%s fork is not supported, skipping test case (%s).", fork, file)

		return
	}

	env := c.Env.ToEnv(t)

	var baseFee *big.Int

	if config.IsActive(chain.London, 0) {
		if c.Env.BaseFee != "" {
			baseFee = stringToBigIntT(t, c.Env.BaseFee)
		} else {
			// Retesteth uses `10` for genesis baseFee. Therefore, it defaults to
			// parent - 2 : 0xa as the basefee for 'this' context.
			baseFee = big.NewInt(testGenesisBaseFee)
		}
	}

	msg, err := c.Transaction.At(p.Indexes, baseFee)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	s, snapshot, pastRoot, err := buildState(c.Pre)
	require.NoError(t, err)

	forks := config.At(uint64(env.Number))

	executor := state.NewExecutor(&chain.Params{
		Forks:   config,
		ChainID: 1,
		BurnContract: map[uint64]types.Address{
			0: types.ZeroAddress,
		},
	}, s, hclog.NewNullLogger())

	executor.PostHook = func(t *state.Transition) {
		if name == "failed_tx_xcf416c53" {
			// create the account
			t.Txn().TouchAccount(ripemd)
			// now remove it
			t.Txn().Suicide(ripemd)
		}
	}
	executor.GetHash = func(*types.Header) func(i uint64) types.Hash {
		return vmTestBlockHash
	}

	transition, err := executor.BeginTxn(pastRoot, c.Env.ToHeader(t), env.Coinbase)
	require.NoError(t, err)
	transition.Apply(msg) //nolint:errcheck

	txn := transition.Txn()

	// mining rewards
	txn.AddSealingReward(env.Coinbase, big.NewInt(0))

	objs, err := txn.Commit(forks.EIP155)
	require.NoError(t, err)

	_, root, err := snapshot.Commit(objs)
	require.NoError(t, err)

	// Check block root
	if !bytes.Equal(root, p.Root.Bytes()) {
		t.Fatalf(
			"root mismatch (%s %s case#%d): expected %s but found %s",
			file,
			fork,
			index,
			p.Root.String(),
			hex.EncodeToHex(root),
		)
	}

	// Check transaction logs
	if logs := rlpHashLogs(txn.Logs()); logs != p.Logs {
		t.Fatalf(
			"logs mismatch (%s %s case#%d): expected %s but found %s",
			file,
			fork,
			index,
			p.Logs.String(),
			logs.String(),
		)
	}
}

func TestState(t *testing.T) {
	t.Parallel()

	long := []string{
		"static_Call50000",
		"static_Return50000",
		"static_Call1MB",
		"stQuadraticComplexityTest",
		"stTimeConsuming",
	}

	skip := []string{
		"RevertPrecompiledTouch",
	}

	// There are two folders in spec tests, one for the current tests for the Istanbul fork
	// and one for the legacy tests for the other forks
	folders, err := listFolders(stateTests)
	if err != nil {
		t.Fatal(err)
	}

	for _, folder := range folders {
		files, err := listFiles(folder)
		if err != nil {
			t.Fatal(err)
		}

		for _, file := range files {
			if !strings.HasSuffix(file, ".json") {
				continue
			}

			if contains(long, file) && testing.Short() {
				t.Skipf("Long tests are skipped in short mode")

				continue
			}

			if contains(skip, file) {
				t.Skip()

				continue
			}

			file := file
			t.Run(file, func(t *testing.T) {
				t.Parallel()

				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}

				var testCases map[string]testCase
				if err = json.Unmarshal(data, &testCases); err != nil {
					t.Fatalf("failed to unmarshal %s: %v", file, err)
				}

				for name, tc := range testCases {
					for fork, postState := range tc.Post {
						for idx, postStateEntry := range postState {
							RunSpecificTest(t, file, tc, name, fork, idx, postStateEntry)
						}
					}
				}
			})
		}
	}
}
