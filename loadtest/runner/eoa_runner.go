package runner

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo"
)

// EOARunner represents a runner for executing load tests specific to EOAs (Externally Owned Accounts).
type EOARunner struct {
	*BaseLoadTestRunner
}

// NewEOARunner creates a new EOARunner instance with the given LoadTestConfig.
// It returns a pointer to the created EOARunner and an error, if any.
func NewEOARunner(cfg LoadTestConfig) (*EOARunner, error) {
	runner, err := NewBaseLoadTestRunner(cfg)
	if err != nil {
		return nil, err
	}

	return &EOARunner{runner}, nil
}

// Run executes the EOA load test.
// It performs the following steps:
// 1. Creates virtual users (VUs).
// 2. Funds the VUs with native tokens.
// 3. Sends EOA transactions using the VUs.
// 4. Waits for the transaction pool to empty.
// 5. Waits for transaction receipts.
// 6. Calculates the transactions per second (TPS) based on block information and transaction statistics.
// Returns an error if any of the steps fail.
func (e *EOARunner) Run() error {
	fmt.Println("Running EOA load test", e.cfg.LoadTestName)

	if err := e.createVUs(); err != nil {
		return err
	}

	if err := e.fundVUs(); err != nil {
		return err
	}

	if !e.cfg.WaitForTxPoolToEmpty {
		go e.waitForReceiptsParallel()
		go e.calculateResultsParallel()

		_, err := e.sendTransactions(e.createEOATransaction)
		if err != nil {
			return err
		}

		return <-e.done
	}

	txHashes, err := e.sendTransactions(e.createEOATransaction)
	if err != nil {
		return err
	}

	if err := e.waitForTxPoolToEmpty(); err != nil {
		return err
	}

	return e.calculateResults(e.waitForReceipts(txHashes))
}

// createEOATransaction creates an EOA transaction
func (e *EOARunner) createEOATransaction(account *account, feeData *feeData,
	chainID *big.Int) *types.Transaction {
	if e.cfg.DynamicTxs {
		return types.NewTx(types.NewDynamicFeeTx(
			types.WithNonce(account.nonce),
			types.WithTo(&receiverAddr),
			types.WithValue(ethgo.Gwei(1)),
			types.WithGas(21000),
			types.WithFrom(account.key.Address()),
			types.WithGasFeeCap(feeData.gasFeeCap),
			types.WithGasTipCap(feeData.gasTipCap),
			types.WithChainID(chainID),
		))
	}

	return types.NewTx(types.NewLegacyTx(
		types.WithNonce(account.nonce),
		types.WithTo(&receiverAddr),
		types.WithValue(ethgo.Gwei(1)),
		types.WithGas(21000),
		types.WithGasPrice(feeData.gasPrice),
		types.WithFrom(account.key.Address()),
	))
}
