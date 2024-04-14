package runner

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/schollz/progressbar/v3"
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

		_, err := e.sendTransactions()
		if err != nil {
			return err
		}

		return <-e.done
	}

	txHashes, err := e.sendTransactions()
	if err != nil {
		return err
	}

	if err := e.waitForTxPoolToEmpty(); err != nil {
		return err
	}

	return e.calculateResults(e.waitForReceipts(txHashes))
}

// sendTransactions sends transactions for the load test.
func (e *EOARunner) sendTransactions() ([]types.Hash, error) {
	return e.BaseLoadTestRunner.sendTransactions(e.sendTransactionsForUser)
}

// sendTransactionsForUser sends multiple transactions for a user account on a specific chain.
// It uses the provided client and chain ID to send transactions using either dynamic or legacy fee models.
// For each transaction, it increments the account's nonce and returns the transaction hashes.
// If an error occurs during the transaction sending process, it returns the error.
func (e *EOARunner) sendTransactionsForUser(account *account, chainID *big.Int,
	bar *progressbar.ProgressBar) ([]types.Hash, []error, error) {
	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithClient(e.client),
		txrelayer.WithChainID(chainID),
		txrelayer.WithCollectTxnHashes(),
		txrelayer.WithNoWaiting(),
		txrelayer.WithoutNonceGet(),
	)
	if err != nil {
		return nil, nil, err
	}

	feeData, err := getFeeData(e.client, e.cfg.DynamicTxs)
	if err != nil {
		return nil, nil, err
	}

	sendErrs := make([]error, 0)
	checkFeeDataNum := e.cfg.TxsPerUser / 5

	for i := 0; i < e.cfg.TxsPerUser; i++ {
		var err error

		if i%checkFeeDataNum == 0 {
			feeData, err = getFeeData(e.client, e.cfg.DynamicTxs)
			if err != nil {
				return nil, nil, err
			}
		}

		if e.cfg.DynamicTxs {
			_, err = txRelayer.SendTransaction(types.NewTx(types.NewDynamicFeeTx(
				types.WithNonce(account.nonce),
				types.WithTo(&receiverAddr),
				types.WithValue(ethgo.Gwei(1)),
				types.WithGas(21000),
				types.WithFrom(account.key.Address()),
				types.WithGasFeeCap(feeData.gasFeeCap),
				types.WithGasTipCap(feeData.gasTipCap),
				types.WithChainID(chainID),
			)), account.key)
		} else {
			_, err = txRelayer.SendTransaction(types.NewTx(types.NewLegacyTx(
				types.WithNonce(account.nonce),
				types.WithTo(&receiverAddr),
				types.WithValue(ethgo.Gwei(1)),
				types.WithGas(21000),
				types.WithGasPrice(feeData.gasPrice),
				types.WithFrom(account.key.Address()),
			)), account.key)
		}

		if err != nil {
			sendErrs = append(sendErrs, err)
		}

		account.nonce++
		_ = bar.Add(1)
	}

	return txRelayer.GetTxnHashes(), sendErrs, nil
}
