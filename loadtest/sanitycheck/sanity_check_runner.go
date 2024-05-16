package sanitycheck

import (
	"fmt"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/umbracle/ethgo/wallet"
)

const (
	passed      = "PASSED"
	failed      = "FAILED"
	uxSeparator = "============================================================="
)

// SanityCheckTestConfig represents the configuration for sanity check tests.
type SanityCheckTestConfig struct {
	Mnemonnic string // Mnemonnic is the mnemonic phrase used for account funding.

	JSONRPCUrl      string        // JSONRPCUrl is the URL of the JSON-RPC server.
	ReceiptsTimeout time.Duration // ReceiptsTimeout is the timeout for waiting for transaction receipts.

	ValidatorKeys []string // ValidatorKeys is the list of private keys of validators.

	ResultsToJSON bool // ResultsToJSON indicates whether the results should be written in JSON format.

	EpochSize uint64 // EpochSize is the size of the epoch.
}

// SanityCheckTestRunner represents a runner for sanity check tests on a test network
type SanityCheckTestRunner struct {
	config *SanityCheckTestConfig

	testAccountKey *crypto.ECDSAKey
	client         *jsonrpc.EthClient

	tests []SanityCheckTest
}

// NewSanityCheckTestRunner creates a new SanityCheckTestRunner
func NewSanityCheckTestRunner(cfg *SanityCheckTestConfig) (*SanityCheckTestRunner, error) {
	key, err := wallet.NewWalletFromMnemonic(cfg.Mnemonnic)
	if err != nil {
		return nil, err
	}

	raw, err := key.MarshallPrivateKey()
	if err != nil {
		return nil, err
	}

	ecdsaKey, err := crypto.NewECDSAKeyFromRawPrivECDSA(raw)
	if err != nil {
		return nil, err
	}

	client, err := jsonrpc.NewEthClient(cfg.JSONRPCUrl)
	if err != nil {
		return nil, err
	}

	tests, err := registerTests(cfg, ecdsaKey, client)
	if err != nil {
		return nil, err
	}

	return &SanityCheckTestRunner{
		config:         cfg,
		tests:          tests,
		client:         client,
		testAccountKey: ecdsaKey,
	}, nil
}

// registerTests registers the sanity check tests that will be run by the SanityCheckTestRunner.
func registerTests(cfg *SanityCheckTestConfig, testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) ([]SanityCheckTest, error) {
	stakeTest, err := NewStakeTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	return []SanityCheckTest{
		stakeTest,
	}, nil
}

// Close closes the BaseLoadTestRunner by closing the underlying client connection.
// It returns an error if there was a problem closing the connection.
func (r *SanityCheckTestRunner) Close() error {
	return r.client.Close()
}

// Run executes the sanity check test based on the provided SanityCheckTestConfig.
func (r *SanityCheckTestRunner) Run() {
	fmt.Println("Running sanity check tests")

	results := make([]string, 0, len(r.tests))

	for _, test := range r.tests {
		result := passed
		t := time.Now().UTC()

		if err := test.Run(); err != nil {
			result = fmt.Sprintf("%s: %v", failed, err)
		}

		results = append(results, fmt.Sprintf("%s: Execution time: %s. Result: %s", test.Name(), time.Since(t), result))
	}

	printUxSeparator()
	fmt.Println("Sanity check results:")
	for _, result := range results {
		fmt.Println(result)
	}
}

func printUxSeparator() {
	fmt.Println(uxSeparator)
}
