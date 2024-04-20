package runner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/wallet"
	"golang.org/x/sync/errgroup"
)

const emptyBlocksNum = 10

type stats struct {
	totalTxs    int
	blockInfo   map[uint64]*BlockInfo
	foundErrors []error
}

type feeData struct {
	gasPrice  *big.Int
	gasTipCap *big.Int
	gasFeeCap *big.Int
}

// BaseLoadTestRunner represents a base load test runner.
type BaseLoadTestRunner struct {
	cfg LoadTestConfig

	loadTestAccount *account
	vus             []*account

	client *jsonrpc.EthClient

	resultsCollectedCh chan *stats
	done               chan error

	batchSender *TransactionBatchSender
}

// NewBaseLoadTestRunner creates a new instance of BaseLoadTestRunner with the provided LoadTestConfig.
// It initializes the load test runner with the given configuration, including the mnemonic for the wallet,
// and sets up the necessary components such as the Ethereum key, binary path, and JSON-RPC client.
// If any error occurs during the initialization process, it returns nil and the error.
// Otherwise, it returns a pointer to the initialized BaseLoadTestRunner and nil error.
func NewBaseLoadTestRunner(cfg LoadTestConfig) (*BaseLoadTestRunner, error) {
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

	return &BaseLoadTestRunner{
		cfg:                cfg,
		loadTestAccount:    &account{key: ecdsaKey},
		client:             client,
		resultsCollectedCh: make(chan *stats),
		done:               make(chan error),
		batchSender:        newTransactionBatchSender(cfg.JSONRPCUrl),
	}, nil
}

// Close closes the BaseLoadTestRunner by closing the underlying client connection.
// It returns an error if there was a problem closing the connection.
func (r *BaseLoadTestRunner) Close() error {
	return r.client.Close()
}

// createVUs creates virtual users (VUs) for the load test.
// It generates ECDSA keys for each VU and stores them in the `vus` slice.
// Returns an error if there was a problem generating the keys.
func (r *BaseLoadTestRunner) createVUs() error {
	fmt.Println("=============================================================")

	start := time.Now().UTC()
	bar := progressbar.Default(int64(r.cfg.VUs), "Creating virtual users")

	defer func() {
		_ = bar.Close()

		fmt.Println("Creating virtual users took", time.Since(start))
	}()

	for i := 0; i < r.cfg.VUs; i++ {
		key, err := crypto.GenerateECDSAKey()
		if err != nil {
			return err
		}

		r.vus = append(r.vus, &account{key: key})
		_ = bar.Add(1)
	}

	return nil
}

// fundVUs funds virtual users by transferring a specified amount of Ether to their addresses.
// It uses the provided load test account's private key to sign the transactions.
// The funding process is performed by executing a command-line bridge tool with the necessary arguments.
// The amount to fund is set to 1000 Ether.
// The function returns an error if there was an issue during the funding process.
func (r *BaseLoadTestRunner) fundVUs() error {
	fmt.Println("=============================================================")

	start := time.Now().UTC()
	bar := progressbar.Default(int64(r.cfg.VUs), "Funding virtual users with native tokens")

	defer func() {
		_ = bar.Close()

		fmt.Println("Funding took", time.Since(start))
	}()

	amountToFund := ethgo.Ether(1000)

	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithClient(r.client),
		txrelayer.WithoutNonceGet(),
	)
	if err != nil {
		return err
	}

	nonce, err := r.client.GetNonce(r.loadTestAccount.key.Address(), jsonrpc.PendingBlockNumberOrHash)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(context.Background())

	for i, vu := range r.vus {
		i := i
		vu := vu

		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				to := vu.key.Address()
				tx := types.NewTx(types.NewLegacyTx(
					types.WithTo(&to),
					types.WithNonce(nonce+uint64(i)),
					types.WithFrom(r.loadTestAccount.key.Address()),
					types.WithValue(amountToFund),
					types.WithGas(21000),
				))

				receipt, err := txRelayer.SendTransaction(tx, r.loadTestAccount.key)
				if err != nil {
					return err
				}

				if receipt == nil || receipt.Status != uint64(types.ReceiptSuccess) {
					return fmt.Errorf("failed to mint ERC20 tokens to %s", vu.key.Address())
				}

				_ = bar.Add(1)

				return nil
			}
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	return nil
}

// waitForTxPoolToEmpty waits for the transaction pool to become empty.
// It continuously checks the status of the transaction pool and returns
// when there are no pending or queued transactions.
// If the transaction pool does not become empty within the specified timeout,
// it returns an error.
func (r *BaseLoadTestRunner) waitForTxPoolToEmpty() error {
	fmt.Println("=============================================================")
	fmt.Println("Waiting for tx pool to empty...")

	timer := time.NewTimer(r.cfg.TxPoolTimeout)
	defer timer.Stop()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			txPoolStatus, err := r.client.TxPoolStatus()
			if err != nil {
				return err
			}

			fmt.Println("Tx pool content. Pending:", txPoolStatus.Pending, "Queued:", txPoolStatus.Queued)

			if txPoolStatus.Pending == 0 && txPoolStatus.Queued == 0 {
				return nil
			}

		case <-timer.C:
			return fmt.Errorf("timeout while waiting for tx pool to empty")
		}
	}
}

// waitForReceiptsParallel waits for the receipts of the given transaction hashes in in a separate go routine.
// It continuously checks for the receipts until they are found or the timeout is reached.
// If the receipts are found, it sends the transaction statistics to the resultsCollectedCh channel.
// If the timeout is reached before the receipts are found, it returns.
// if there is a predefined number of empty blocks, it stops the results gathering before the timer.
func (r *BaseLoadTestRunner) waitForReceiptsParallel() {
	startBlock, err := r.client.BlockNumber()
	if err != nil {
		fmt.Println("Error getting start block on gathering block info:", err)

		return
	}

	currentBlock := startBlock
	blockInfoMap := make(map[uint64]*BlockInfo)
	foundErrors := make([]error, 0)
	sequentialEmptyBlocks := 0
	totalTxsExecuted := 0

	timer := time.NewTimer(30 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)

	defer func() {
		timer.Stop()
		ticker.Stop()
		fmt.Println("Gathering results in parallel finished.")
		r.resultsCollectedCh <- &stats{totalTxs: totalTxsExecuted, blockInfo: blockInfoMap, foundErrors: foundErrors}
	}()

	for {
		select {
		case <-timer.C:
			fmt.Println("Timeout while gathering block info")

			return
		case <-ticker.C:
			if sequentialEmptyBlocks >= emptyBlocksNum {
				return
			}

			block, err := r.client.GetBlockByNumber(jsonrpc.BlockNumber(currentBlock), true)
			if err != nil {
				foundErrors = append(foundErrors, err)

				continue
			}

			if block == nil {
				continue
			}

			if (len(block.Transactions) == 1 && block.Transactions[0].Type() == types.StateTxType) ||
				len(block.Transactions) == 0 {
				sequentialEmptyBlocks++
				currentBlock++

				continue
			}

			sequentialEmptyBlocks = 0

			gasUsed := new(big.Int).SetUint64(block.Header.GasUsed)
			gasLimit := new(big.Int).SetUint64(block.Header.GasLimit)
			gasUtilization := new(big.Int).Mul(gasUsed, big.NewInt(10000))
			gasUtilization = gasUtilization.Div(gasUtilization, gasLimit).Div(gasUtilization, big.NewInt(100))

			gu, _ := gasUtilization.Float64()

			blockInfoMap[block.Number()] = &BlockInfo{
				Number:         block.Number(),
				CreatedAt:      block.Header.Timestamp,
				NumTxs:         len(block.Transactions),
				GasUsed:        new(big.Int).SetUint64(block.Header.GasUsed),
				GasLimit:       new(big.Int).SetUint64(block.Header.GasLimit),
				GasUtilization: gu,
			}

			totalTxsExecuted += len(block.Transactions)
			currentBlock++
		}
	}
}

// waitForReceipts waits for the receipts of the given transaction hashes and returns
// a map of block information, transaction statistics, and an error if any.
func (r *BaseLoadTestRunner) waitForReceipts(txHashes []types.Hash) (map[uint64]*BlockInfo, int) {
	fmt.Println("=============================================================")

	start := time.Now().UTC()
	blockInfoMap := make(map[uint64]*BlockInfo)
	txToBlockMap := make(map[types.Hash]uint64)
	bar := progressbar.Default(int64(len(txHashes)), "Gathering receipts")

	defer func() {
		_ = bar.Close()

		fmt.Println("Waiting for receipts took", time.Since(start))
	}()

	foundErrors := make([]error, 0)

	var lock sync.Mutex

	getTxReceipts := func(txHashes []types.Hash) {
		for _, txHash := range txHashes {
			lock.Lock()
			if _, exists := txToBlockMap[txHash]; exists {
				_ = bar.Add(1)
				lock.Unlock()

				continue
			}

			lock.Unlock()

			receipt, err := r.waitForReceipt(txHash)
			if err != nil {
				lock.Lock()
				foundErrors = append(foundErrors, err)
				lock.Unlock()

				continue
			}

			_ = bar.Add(1)

			block, err := r.client.GetBlockByNumber(jsonrpc.BlockNumber(receipt.BlockNumber), true)
			if err != nil {
				lock.Lock()
				foundErrors = append(foundErrors, err)
				lock.Unlock()

				continue
			}

			gasUsed := new(big.Int).SetUint64(block.Header.GasUsed)
			gasLimit := new(big.Int).SetUint64(block.Header.GasLimit)
			gasUtilization := new(big.Int).Mul(gasUsed, big.NewInt(10000))
			gasUtilization = gasUtilization.Div(gasUtilization, gasLimit).Div(gasUtilization, big.NewInt(100))

			gu, _ := gasUtilization.Float64()

			lock.Lock()
			blockInfoMap[receipt.BlockNumber] = &BlockInfo{
				Number:         receipt.BlockNumber,
				CreatedAt:      block.Header.Timestamp,
				NumTxs:         len(block.Transactions),
				GasUsed:        new(big.Int).SetUint64(block.Header.GasUsed),
				GasLimit:       new(big.Int).SetUint64(block.Header.GasLimit),
				GasUtilization: gu,
			}

			for _, txn := range block.Transactions {
				txToBlockMap[txn.Hash()] = receipt.BlockNumber
			}
			lock.Unlock()
		}
	}

	totalTxns := len(txHashes)

	// split the txHashes into batches so we can get them in parallel routines
	batchSize := totalTxns / 10
	if batchSize == 0 {
		batchSize = 1
	}

	var wg sync.WaitGroup

	for i := 0; i < totalTxns; i += batchSize {
		end := i + batchSize
		if end > totalTxns {
			end = totalTxns
		}

		wg.Add(1)

		go func(txHashes []types.Hash) {
			defer wg.Done()

			getTxReceipts(txHashes)
		}(txHashes[i:end])
	}

	wg.Wait()

	if len(foundErrors) > 0 {
		fmt.Println("Errors found while waiting for receipts:")

		for _, err := range foundErrors {
			fmt.Println(err)
		}
	}

	return blockInfoMap, len(txHashes)
}

// waitForReceipt waits for the transaction receipt of the given transaction hash.
// It continuously checks for the receipt until it is found or the timeout is reached.
// If the receipt is found, it returns the receipt and nil error.
// If the timeout is reached before the receipt is found, it returns nil receipt and an error.
func (r *BaseLoadTestRunner) waitForReceipt(txHash types.Hash) (*ethgo.Receipt, error) {
	timer := time.NewTimer(r.cfg.ReceiptsTimeout)
	defer timer.Stop()

	tickerTimeout := time.Second
	if r.cfg.ReceiptsTimeout <= time.Second {
		tickerTimeout = r.cfg.ReceiptsTimeout / 2
	}

	ticker := time.NewTicker(tickerTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			receipt, err := r.client.GetTransactionReceipt(txHash)
			if err != nil {
				if err.Error() != "not found" {
					return nil, err
				}
			}

			if receipt != nil {
				return receipt, nil
			}
		case <-timer.C:
			return nil, fmt.Errorf("timeout while waiting for transaction %s to be processed", txHash)
		}
	}
}

// calculateResultsParallel calculates the results of load test.
// Should be used in a separate go routine.
func (r *BaseLoadTestRunner) calculateResultsParallel() {
	stats := <-r.resultsCollectedCh

	if len(stats.foundErrors) > 0 {
		fmt.Println("Errors found while gathering results:")

		for _, err := range stats.foundErrors {
			fmt.Println(err)
		}
	}

	r.done <- r.calculateResults(stats.blockInfo, stats.totalTxs)
}

// calculateResults calculates the results of a load test for a given set of
// block information and transaction statistics.
// It takes a map of block information and an array of transaction statistics as input.
// The function iterates over the transaction statistics and calculates the TPS for each block.
// It also calculates the minimum and maximum TPS values, as well as the total time taken to mine the transactions.
// The calculated TPS values are displayed in a table using the tablewriter package.
// The function returns an error if there is any issue retrieving block information or calculating TPS.
func (r *BaseLoadTestRunner) calculateResults(blockInfos map[uint64]*BlockInfo, totalTxs int) error {
	fmt.Println("=============================================================")
	fmt.Println("Calculating results...")

	var (
		totalTime           float64
		maxTxsPerSecond     float64
		minTxsPerSecond     = math.MaxFloat64
		blockTimeMap        = make(map[uint64]uint64)
		uniqueBlocks        = map[uint64]struct{}{}
		infos               = make([]*BlockInfo, 0, len(blockInfos))
		totalGasUsed        = big.NewInt(0)
		minGasUtilization   = math.MaxFloat64
		maxGasUtilization   float64
		totalGasUtilization float64
	)

	for num, stat := range blockInfos {
		uniqueBlocks[num] = struct{}{}

		infos = append(infos, stat)
	}

	for block := range uniqueBlocks {
		currentBlockTxsNum := 0
		parentBlockNum := block - 1

		if _, exists := blockTimeMap[parentBlockNum]; !exists {
			if parentBlockInfo, exists := blockInfos[parentBlockNum]; !exists {
				parentBlock, err := r.client.GetBlockByNumber(jsonrpc.BlockNumber(parentBlockNum), false)
				if err != nil {
					return err
				}

				blockTimeMap[parentBlockNum] = parentBlock.Header.Timestamp
			} else {
				blockTimeMap[parentBlockNum] = parentBlockInfo.CreatedAt
			}
		}

		parentBlockTimestamp := blockTimeMap[parentBlockNum]

		if _, ok := blockTimeMap[block]; !ok {
			if currentBlockInfo, ok := blockInfos[block]; !ok {
				currentBlock, err := r.client.GetBlockByNumber(jsonrpc.BlockNumber(parentBlockNum), true)
				if err != nil {
					return err
				}

				blockTimeMap[block] = currentBlock.Header.Timestamp
				currentBlockTxsNum = len(currentBlock.Transactions)
			} else {
				blockTimeMap[block] = currentBlockInfo.CreatedAt
				currentBlockTxsNum = currentBlockInfo.NumTxs
			}
		}

		if currentBlockTxsNum == 0 {
			currentBlockTxsNum = blockInfos[block].NumTxs
		}

		currentBlockTimestamp := blockTimeMap[block]
		blockTime := math.Abs(float64(currentBlockTimestamp - parentBlockTimestamp))

		currentBlockTxsPerSecond := float64(currentBlockTxsNum) / blockTime

		if currentBlockTxsPerSecond > maxTxsPerSecond {
			maxTxsPerSecond = currentBlockTxsPerSecond
		}

		if currentBlockTxsPerSecond < minTxsPerSecond {
			minTxsPerSecond = currentBlockTxsPerSecond
		}

		if blockInfos[block].GasUtilization < minGasUtilization {
			minGasUtilization = blockInfos[block].GasUtilization
		}

		if blockInfos[block].GasUtilization > maxGasUtilization {
			maxGasUtilization = blockInfos[block].GasUtilization
		}

		totalTime += blockTime
		totalGasUtilization += blockInfos[block].GasUtilization
		totalGasUsed.Add(totalGasUsed, blockInfos[block].GasUsed)
	}

	for _, info := range blockInfos {
		info.BlockTime = math.Abs(float64(info.CreatedAt - blockTimeMap[info.Number-1]))
		info.TPS = float64(info.NumTxs) / info.BlockTime
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Number < infos[j].Number
	})

	avgTxsPerSecond := math.Ceil(float64(totalTxs) / totalTime)
	avgGasPerTx := new(big.Int).Div(totalGasUsed, big.NewInt(int64(totalTxs)))
	avgGasUtilization := totalGasUtilization / float64(len(blockInfos))

	if !r.cfg.ResultsToJSON {
		return printResults(
			totalTxs, totalTime, totalGasUsed,
			maxTxsPerSecond, minTxsPerSecond, avgTxsPerSecond, avgGasPerTx,
			minGasUtilization, maxGasUtilization, avgGasUtilization,
			infos,
		)
	}

	return r.saveResultsToJSONFile(
		totalTxs, totalTime, totalGasUsed,
		maxTxsPerSecond, minTxsPerSecond, avgTxsPerSecond, avgGasPerTx,
		minGasUtilization, maxGasUtilization, avgGasUtilization,
		infos,
	)
}

// saveResultsToJSONFile saves the load test results to a JSON file.
// It takes the total number of transactions (totalTxs), total time taken (totalTime),
// maximum transactions per second (maxTxsPerSecond), minimum transactions per second (minTxsPerSecond),
// average transactions per second (avgTxsPerSecond), and a map of block information (blockInfos).
// It returns an error if there was a problem saving the results to the file.
func (r *BaseLoadTestRunner) saveResultsToJSONFile(
	totalTxs int, totalTime float64, totalGasUsed *big.Int,
	maxTxsPerSecond, minTxsPerSecond, avgTxsPerSecond float64, avgGasPerTx *big.Int,
	minGasUtilization, maxGasUtilization, avgGasUtilization float64,
	blockInfos []*BlockInfo) error {
	fmt.Println("Saving results to JSON file...")

	type Result struct {
		TotalBlocks       int          `json:"totalBlocks"`
		TotalTxs          int          `json:"totalTxs"`
		TotalTime         float64      `json:"totalTime"`
		TotalGasUsed      string       `json:"totalGasUsed"`
		MinTxsPerSecond   float64      `json:"minTxsPerSecond"`
		MaxTxsPerSecond   float64      `json:"maxTxsPerSecond"`
		AvgTxsPerSecond   float64      `json:"avgTxsPerSecond"`
		AvgGasPerTx       string       `json:"avgGasPerTx"`
		MinGasUtilization float64      `json:"minGasUtilization"`
		MaxGasUtilization float64      `json:"maxGasUtilization"`
		AvgGasUtilization float64      `json:"avgGasUtilization"`
		Blocks            []*BlockInfo `json:"blocks"`
	}

	result := Result{
		TotalBlocks:       len(blockInfos),
		TotalTxs:          totalTxs,
		TotalTime:         totalTime,
		TotalGasUsed:      totalGasUsed.String(),
		MinTxsPerSecond:   minTxsPerSecond,
		MaxTxsPerSecond:   maxTxsPerSecond,
		AvgTxsPerSecond:   avgTxsPerSecond,
		Blocks:            blockInfos,
		AvgGasPerTx:       avgGasPerTx.String(),
		MinGasUtilization: minGasUtilization,
		MaxGasUtilization: maxGasUtilization,
		AvgGasUtilization: avgGasUtilization,
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		return err
	}

	fileName := fmt.Sprintf("./%s_%s.json", r.cfg.LoadTestName, r.cfg.LoadTestType)

	err = common.SaveFileSafe(fileName, jsonData, 0600)
	if err != nil {
		return err
	}

	fmt.Println("Results saved to JSON file", fileName)

	return nil
}

// sendTransactions sends transactions for each virtual user (vu) and returns the transaction hashes.
// It retrieves the chain ID from the client and uses it to send transactions for each user.
// The function runs concurrently for each user using errgroup.
// If the context is canceled, the function returns the context error.
// The transaction hashes are appended to the allTxnHashes slice.
// Finally, the function prints the time taken to send the transactions
// and returns the transaction hashes and nil error.
func (r *BaseLoadTestRunner) sendTransactions(createTxnFn func(*account, *feeData, *big.Int) *types.Transaction,
) ([]types.Hash, error) {
	fmt.Println("=============================================================")

	chainID, err := r.client.ChainID()
	if err != nil {
		return nil, err
	}

	start := time.Now().UTC()
	totalTxs := r.cfg.VUs * r.cfg.TxsPerUser
	foundErrs := make([]error, 0)
	bar := progressbar.Default(int64(totalTxs), "Sending transactions")

	defer func() {
		_ = bar.Close()

		fmt.Println("Sending transactions took", time.Since(start))
	}()

	allTxnHashes := make([]types.Hash, 0)

	g, ctx := errgroup.WithContext(context.Background())

	sendFn := r.sendTransactionsForUser
	if r.cfg.BatchSize > 1 {
		sendFn = r.sendTransactionsForUserInBatches
	}

	for _, vu := range r.vus {
		vu := vu

		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()

			default:
				txnHashes, sendErrors, err := sendFn(vu, chainID, bar, createTxnFn)
				if err != nil {
					return err
				}

				foundErrs = append(foundErrs, sendErrors...)
				allTxnHashes = append(allTxnHashes, txnHashes...)

				return nil
			}
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	if len(foundErrs) > 0 {
		fmt.Println("Errors found while sending transactions:")

		for _, err := range foundErrs {
			fmt.Println(err)
		}
	}

	return allTxnHashes, nil
}

// sendTransactionsForUser sends ERC20 token transactions for a given user account.
// It takes an account pointer and a chainID as input parameters.
// It returns a slice of transaction hashes and an error if any.
func (r *BaseLoadTestRunner) sendTransactionsForUser(account *account, chainID *big.Int,
	bar *progressbar.ProgressBar, createTxnFn func(*account, *feeData, *big.Int) *types.Transaction,
) ([]types.Hash, []error, error) {
	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithClient(r.client),
		txrelayer.WithChainID(chainID),
		txrelayer.WithCollectTxnHashes(),
		txrelayer.WithNoWaiting(),
		txrelayer.WithEstimateGasFallback(),
		txrelayer.WithoutNonceGet(),
	)
	if err != nil {
		return nil, nil, err
	}

	feeData, err := getFeeData(r.client, r.cfg.DynamicTxs)
	if err != nil {
		return nil, nil, err
	}

	sendErrs := make([]error, 0)
	checkFeeDataNum := r.cfg.TxsPerUser / 5

	for i := 0; i < r.cfg.TxsPerUser; i++ {
		if i%checkFeeDataNum == 0 {
			feeData, err = getFeeData(r.client, r.cfg.DynamicTxs)
			if err != nil {
				return nil, nil, err
			}
		}

		_, err = txRelayer.SendTransaction(createTxnFn(account, feeData, chainID), account.key)
		if err != nil {
			sendErrs = append(sendErrs, err)
		}

		account.nonce++
		_ = bar.Add(1)
	}

	return txRelayer.GetTxnHashes(), sendErrs, nil
}

// sendTransactionsForUserInBatches sends user transactions in batches to the rpc node
func (r *BaseLoadTestRunner) sendTransactionsForUserInBatches(account *account, chainID *big.Int,
	bar *progressbar.ProgressBar, createTxnFn func(*account, *feeData, *big.Int) *types.Transaction,
) ([]types.Hash, []error, error) {
	signer := crypto.NewLondonSigner(chainID.Uint64())

	numOfBatches := int(math.Ceil(float64(r.cfg.TxsPerUser) / float64(r.cfg.BatchSize)))
	txHashes := make([]types.Hash, 0, r.cfg.TxsPerUser)
	sendErrs := make([]error, 0)
	totalTxs := 0

	var gas uint64

	feeData, err := getFeeData(r.client, r.cfg.DynamicTxs)
	if err != nil {
		return nil, nil, err
	}

	txnExample := createTxnFn(account, feeData, chainID)
	if txnExample.Gas() == 0 {
		// estimate gas initially
		gasLimit, err := r.client.EstimateGas(txrelayer.ConvertTxnToCallMsg(txnExample))
		if err != nil {
			gasLimit = txrelayer.DefaultGasLimit
		}

		gas = gasLimit * 2 // double it just in case
	} else {
		gas = txnExample.Gas()
	}

	for i := 0; i < numOfBatches; i++ {
		batchTxs := make([]string, 0, r.cfg.BatchSize)

		feeData, err := getFeeData(r.client, r.cfg.DynamicTxs)
		if err != nil {
			return nil, nil, err
		}

		for j := 0; j < r.cfg.BatchSize; j++ {
			if totalTxs >= r.cfg.TxsPerUser {
				break
			}

			txn := createTxnFn(account, feeData, chainID)
			if txn.Gas() == 0 {
				txn.SetGas(gas)
			}

			signedTxn, err := signer.SignTxWithCallback(txn,
				func(hash types.Hash) (sig []byte, err error) {
					return account.key.Sign(hash.Bytes())
				})
			if err != nil {
				sendErrs = append(sendErrs, err)

				continue
			}

			batchTxs = append(batchTxs, "0x"+hex.EncodeToString(signedTxn.MarshalRLP()))
			account.nonce++
			totalTxs++
		}

		hashes, err := r.batchSender.SendBatch(batchTxs)
		if err != nil {
			return nil, nil, err
		}

		txHashes = append(txHashes, hashes...)
		_ = bar.Add(len(batchTxs))
	}

	return txHashes, sendErrs, nil
}

// getFeeData retrieves fee data based on the provided JSON-RPC Ethereum client and dynamicTxs flag.
// If dynamicTxs is true, it calculates the gasTipCap and gasFeeCap based on the MaxPriorityFeePerGas,
// FeeHistory, and BaseFee values obtained from the client. If dynamicTxs is false, it calculates the
// gasPrice based on the GasPrice value obtained from the client.
// The function returns a feeData struct containing the calculated fee values.
// If an error occurs during the retrieval or calculation, the function returns nil and the error.
func getFeeData(client *jsonrpc.EthClient, dynamicTxs bool) (*feeData, error) {
	feeData := &feeData{}

	if dynamicTxs {
		mpfpg, err := client.MaxPriorityFeePerGas()
		if err != nil {
			return nil, err
		}

		gasTipCap := new(big.Int).Mul(mpfpg, big.NewInt(2))

		feeHistory, err := client.FeeHistory(1, jsonrpc.LatestBlockNumber, nil)
		if err != nil {
			return nil, err
		}

		baseFee := big.NewInt(0)

		if len(feeHistory.BaseFee) != 0 {
			baseFee = baseFee.SetUint64(feeHistory.BaseFee[len(feeHistory.BaseFee)-1])
		}

		gasFeeCap := new(big.Int).Add(baseFee, mpfpg)
		gasFeeCap.Mul(gasFeeCap, big.NewInt(2))

		feeData.gasTipCap = gasTipCap
		feeData.gasFeeCap = gasFeeCap
	} else {
		gp, err := client.GasPrice()
		if err != nil {
			return nil, err
		}

		gasPrice := new(big.Int).SetUint64(gp + (gp * 50 / 100))

		feeData.gasPrice = gasPrice
	}

	return feeData, nil
}

// printResults prints the results of the load test to stdout in a form of a table
func printResults(totalTxs int, totalTime float64, totalGasUsed *big.Int,
	maxTxsPerSecond, minTxsPerSecond, avgTxsPerSecond float64, avgGasPerTx *big.Int,
	minGasUtilization, maxGasUtilization, avgGasUtilization float64,
	blockInfos []*BlockInfo) error {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"Block Number",
		"Block Time (s)",
		"Num Txs",
		"Gas Used",
		"Gas Limit",
		"Gas Utilization",
		"TPS",
	})

	for _, blockInfo := range blockInfos {
		table.Append([]string{
			fmt.Sprintf("%d", blockInfo.Number),
			fmt.Sprintf("%.2f", blockInfo.BlockTime),
			fmt.Sprintf("%d", blockInfo.NumTxs),
			fmt.Sprintf("%d", blockInfo.GasUsed.Uint64()),
			fmt.Sprintf("%d", blockInfo.GasLimit.Uint64()),
			fmt.Sprintf("%.2f", blockInfo.GasUtilization),
			fmt.Sprintf("%.2f", blockInfo.TPS),
		})
	}

	table.Render()

	table = tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"Total Blocks",
		"Total Txs",
		"Total Time To Mine (s)",
		"Total Gas Used",
		"Average Gas Per Tx",
		"Min TPS",
		"Max TPS",
		"Average TPS",
		"Min Gas Utilization",
		"Max Gas Utilization",
		"Average Gas Utilization",
	})
	table.Append([]string{
		fmt.Sprintf("%d", len(blockInfos)),
		fmt.Sprintf("%d", totalTxs),
		fmt.Sprintf("%.2f", totalTime),
		totalGasUsed.String(),
		avgGasPerTx.String(),
		fmt.Sprintf("%.2f", minTxsPerSecond),
		fmt.Sprintf("%.2f", maxTxsPerSecond),
		fmt.Sprintf("%.2f", avgTxsPerSecond),
		fmt.Sprintf("%.2f", minGasUtilization),
		fmt.Sprintf("%.2f", maxGasUtilization),
		fmt.Sprintf("%.2f", avgGasUtilization),
	})

	table.Render()

	return nil
}
