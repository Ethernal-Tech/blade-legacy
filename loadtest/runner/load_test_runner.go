package runner

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	EOATestType    = "eoa"
	ERC20TestType  = "erc20"
	ERC721TestType = "erc721"
	MixedTestType  = "mixed"
)

var receiverAddr = types.StringToAddress("0xDeaDbeefdEAdbeefdEadbEEFdeadbeEFdEaDbeeF")

func IsLoadTestSupported(loadTestType string) bool {
	ltp := strings.ToLower(loadTestType)

	return ltp == EOATestType || ltp == ERC20TestType || ltp == ERC721TestType || ltp == MixedTestType
}

type account struct {
	nonce uint64
	key   *crypto.ECDSAKey
}

type BlockInfo struct {
	Number    uint64
	CreatedAt uint64
	NumTxs    int

	GasUsed        *big.Int
	GasLimit       *big.Int
	GasUtilization float64

	TPS       float64
	BlockTime float64
}

// LoadTestConfig represents the configuration for a load test.
type LoadTestConfig struct {
	Mnemonnic string // Mnemonnic is the mnemonic phrase used for account generation, and VUs funding.

	LoadTestType string // LoadTestType is the type of load test.
	LoadTestName string // LoadTestName is the name of the load test.

	JSONRPCUrl      string        // JSONRPCUrl is the URL of the JSON-RPC server.
	ReceiptsTimeout time.Duration // ReceiptsTimeout is the timeout for waiting for transaction receipts.
	TxPoolTimeout   time.Duration // TxPoolTimeout is the timeout for waiting for tx pool to empty.

	VUs        int  // VUs is the number of virtual users.
	TxsPerUser int  // TxsPerUser is the number of transactions per user.
	BatchSize  int  // BatchSize is the number of transactions to send in a single batch.
	DynamicTxs bool // DynamicTxs indicates whether the load test should generate dynamic transactions.

	ResultsToJSON        bool // ResultsToJSON indicates whether the results should be written in JSON format.
	WaitForTxPoolToEmpty bool // WaitForTxPoolToEmpty indicates whether the load test
	// should wait for the tx pool to empty before gathering results
}

// LoadTestRunner represents a runner for load tests.
type LoadTestRunner struct{}

// Run executes the load test based on the provided LoadTestConfig.
// It determines the load test type from the configuration and creates
// the corresponding runner. Then, it runs the load test using the
// created runner and returns any error encountered during the process.
func (r *LoadTestRunner) Run(cfg LoadTestConfig) error {
	switch strings.ToLower(cfg.LoadTestType) {
	case EOATestType:
		eoaRunner, err := NewEOARunner(cfg)
		if err != nil {
			return err
		}

		return eoaRunner.Run()
	case ERC20TestType:
		erc20Runner, err := NewERC20Runner(cfg)
		if err != nil {
			return err
		}

		return erc20Runner.Run()
	case ERC721TestType:
		erc721Runner, err := NewERC721Runner(cfg)
		if err != nil {
			return err
		}

		return erc721Runner.Run()
	case MixedTestType:
		mixedTxRunner, err := NewMixedTxRunner(cfg)
		if err != nil {
			return err
		}

		return mixedTxRunner.Run()
	default:
		return fmt.Errorf("unknown load test type %s", cfg.LoadTestType)
	}
}
