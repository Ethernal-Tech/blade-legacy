package runner

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"

	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

var three = big.NewInt(3)

// MixedTxRunner represents a load test runner for sending every type of transaction
// in a single load test
type MixedTxRunner struct {
	*BaseLoadTestRunner
	*ERC20Runner
	*ERC721Runner
	*EOARunner

	lock sync.Mutex

	numOfEOATxs    int
	numOfERC20Txs  int
	numOfERC721Txs int

	erc20Gas  uint64
	erc721Gas uint64
}

// NewMixedTxRunner creates a new MixedTxRunner
func NewMixedTxRunner(cfg LoadTestConfig) (*MixedTxRunner, error) {
	runner, err := NewBaseLoadTestRunner(cfg)
	if err != nil {
		return nil, err
	}

	return &MixedTxRunner{
		BaseLoadTestRunner: runner,
		ERC20Runner:        &ERC20Runner{BaseLoadTestRunner: runner},
		ERC721Runner:       &ERC721Runner{BaseLoadTestRunner: runner},
		EOARunner:          &EOARunner{BaseLoadTestRunner: runner},
	}, nil
}

// Run executes the Mixed load test.
// It performs the following steps:
// 1. Creates virtual users (VUs).
// 2. Funds the VUs with native tokens.
// 3. Deploys the ERC20 token contract.
// 4. Mints ERC20 tokens to the VUs.
// 5. Deploys the ERC721 token contract.
// 6. Sends transactions using the VUs (by creating a transaction of a random type).
// 7. Waits for the transaction pool to empty.
// 8. Waits for transaction receipts.
// 9. Calculates the transactions per second (TPS) based on block information and transaction statistics.
// Returns an error if any of the steps fail.
func (m *MixedTxRunner) Run() error {
	fmt.Println("Running mixed load test", m.cfg.LoadTestName)

	if err := m.createVUs(); err != nil {
		return err
	}

	if err := m.fundVUs(); err != nil {
		return err
	}

	if err := m.deployERC20Token(); err != nil {
		return err
	}

	if err := m.mintERC20TokenToVUs(); err != nil {
		return err
	}

	if err := m.deployERC21Token(); err != nil {
		return err
	}

	if err := m.estimateGas(); err != nil {
		return err
	}

	if !m.cfg.WaitForTxPoolToEmpty {
		go m.waitForReceiptsParallel()
		go m.calculateResultsParallel()

		_, err := m.sendTransactions(m.createTransaction)
		if err != nil {
			return err
		}

		m.printHowManySent()

		return <-m.done
	}

	txHashes, err := m.sendTransactions(m.createTransaction)
	if err != nil {
		return err
	}

	m.printHowManySent()

	if err := m.waitForTxPoolToEmpty(); err != nil {
		return err
	}

	return m.calculateResults(m.waitForReceipts(txHashes))
}

// createTransaction creates a transaction for the mixed load test
func (m *MixedTxRunner) createTransaction(account *account, feeData *feeData, chainID *big.Int) *types.Transaction {
	// Randomly choose a transaction type
	r, _ := rand.Int(rand.Reader, three)

	switch r.Uint64() {
	case 0:
		m.lock.Lock()
		m.numOfERC20Txs++
		m.lock.Unlock()

		tx := m.createERC20Transaction(account, feeData, chainID)
		tx.SetGas(m.erc20Gas)

		return tx
	case 1:
		m.lock.Lock()
		m.numOfERC721Txs++
		m.lock.Unlock()

		tx := m.createERC721Transaction(account, feeData, chainID)
		tx.SetGas(m.erc721Gas)

		return tx
	default:
		m.lock.Lock()
		m.numOfEOATxs++
		m.lock.Unlock()

		return m.createEOATransaction(account, feeData, chainID)
	}
}

// estimateGas estimates the gas for ERC transaction types
func (m *MixedTxRunner) estimateGas() error {
	estimateGasFn := func(tx *types.Transaction) uint64 {
		gasLimit, err := m.client.EstimateGas(txrelayer.ConvertTxnToCallMsg(tx))
		if err != nil {
			gasLimit = txrelayer.DefaultGasLimit
		}

		return gasLimit * 2 // double it just in case
	}

	chainID, err := m.client.ChainID()
	if err != nil {
		return err
	}

	feeData, err := getFeeData(m.client, m.cfg.DynamicTxs)
	if err != nil {
		return err
	}

	m.erc20Gas = estimateGasFn(m.createERC20Transaction(m.loadTestAccount, feeData, chainID))
	m.erc721Gas = estimateGasFn(m.createERC721Transaction(m.loadTestAccount, feeData, chainID))

	return nil
}

// printHowManySent prints how many transactions of each type were sent
func (m *MixedTxRunner) printHowManySent() {
	fmt.Println("=============================================================")

	fmt.Println("EOA created", m.numOfEOATxs)
	fmt.Println("ERC20 created", m.numOfERC20Txs)
	fmt.Println("ERC721 created", m.numOfERC721Txs)
}
