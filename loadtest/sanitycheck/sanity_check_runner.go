package sanitycheck

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/umbracle/ethgo/wallet"
)

const (
	passed      = "PASSED"
	failed      = "FAILED"
	uxSeparator = "============================================================="
)

// SanityCheckTestResult represents the result of a sanity check test.
type SanityCheckTestResult struct {
	Name          string
	ExecutionTime string
	Result        string
	Err           error
}

// String returns a string representation of the SanityCheckTestResult.
func (s SanityCheckTestResult) String() string {
	if s.Err != nil {
		return fmt.Sprintf("%s: Execution time: %s. Error: %s. Result: %s.",
			s.Name, s.ExecutionTime, s.Err.Error(), s.Result)
	}

	return fmt.Sprintf("%s: Execution time: %s. Result: %s.", s.Name, s.ExecutionTime, s.Result)
}

// SanityCheckTestConfig represents the configuration for sanity check tests.
type SanityCheckTestConfig struct {
	Mnemonic string // Mnemonnic is the mnemonic phrase used for account funding.

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
	key, err := wallet.NewWalletFromMnemonic(cfg.Mnemonic)
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
func registerTests(cfg *SanityCheckTestConfig,
	testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) ([]SanityCheckTest, error) {
	stakeTest, err := NewStakeTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	unstakeTest, err := NewUnstakeTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	registerValidatorTest, err := NewRegisterValidatorTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	return []SanityCheckTest{
		stakeTest,
		unstakeTest,
		registerValidatorTest,
	}, nil
}

// Close closes the BaseLoadTestRunner by closing the underlying client connection.
// It returns an error if there was a problem closing the connection.
func (r *SanityCheckTestRunner) Close() error {
	return r.client.Close()
}

// Run executes the sanity check test based on the provided SanityCheckTestConfig.
func (r *SanityCheckTestRunner) Run() error {
	fmt.Println("Running sanity check tests")

	results := make([]SanityCheckTestResult, 0, len(r.tests))

	for _, test := range r.tests {
		result := passed
		t := time.Now().UTC()

		err := test.Run()
		if err != nil {
			result = failed
		}

		results = append(results, SanityCheckTestResult{
			Name:          test.Name(),
			ExecutionTime: time.Since(t).String(),
			Result:        result,
			Err:           err,
		})
	}

	printUxSeparator()
	fmt.Println("Sanity check results:")

	if !r.config.ResultsToJSON {
		for _, result := range results {
			fmt.Println(result.String())
		}

		return nil
	}

	return saveResultsToFile(results, "sanity_check_results.json")
}

// saveResultsToFile saves the sanity check tests results to a JSON file.
func saveResultsToFile(results []SanityCheckTestResult, fileName string) error {
	jsonData, err := json.Marshal(results)
	if err != nil {
		return err
	}

	if err := common.SaveFileSafe(fileName, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to save results to JSON file: %w", err)
	}

	fmt.Println("Results saved to JSON file", fileName)

	return nil
}

// printUxSeparator prints a separator to the console.
func printUxSeparator() {
	fmt.Println(uxSeparator)
}
