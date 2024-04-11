package runner

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"os"
	"sort"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/wallet"
	"golang.org/x/sync/errgroup"
)

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
		cfg:             cfg,
		loadTestAccount: &account{key: ecdsaKey},
		client:          client,
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
		txrelayer.WithReceiptsTimeout(r.cfg.ReceiptsTimeout),
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

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			txPoolStatus, err := r.client.Status()
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

// waitForReceipts waits for the receipts of the given transaction hashes and returns
// a map of block information, transaction statistics, and an error if any.
func (r *BaseLoadTestRunner) waitForReceipts(txHashes []types.Hash) (map[uint64]blockInfo, []txStats) {
	fmt.Println("=============================================================")

	start := time.Now().UTC()
	blockInfoMap := make(map[uint64]blockInfo)
	txToBlockMap := make(map[types.Hash]uint64)
	txnStats := make([]txStats, 0, len(txHashes))
	bar := progressbar.Default(int64(len(txHashes)), "Gathering receipts")

	defer func() {
		_ = bar.Close()
		fmt.Println("Waiting for receipts took", time.Since(start))
	}()

	foundErrors := make([]error, 0)

	for _, txHash := range txHashes {
		if blockNum, exists := txToBlockMap[txHash]; exists {
			txnStats = append(txnStats, txStats{txHash, blockNum})
			_ = bar.Add(1)

			continue
		}

		receipt, err := r.waitForReceipt(txHash)
		if err != nil {
			foundErrors = append(foundErrors, err)

			continue
		}

		txnStats = append(txnStats, txStats{txHash, receipt.BlockNumber})
		_ = bar.Add(1)

		block, err := r.client.GetBlockByNumber(jsonrpc.BlockNumber(receipt.BlockNumber), true)
		if err != nil {
			foundErrors = append(foundErrors, err)

			continue
		}

		gasUsed := new(big.Int).SetUint64(block.Header.GasUsed)
		gasLimit := new(big.Int).SetUint64(block.Header.GasLimit)
		gasUtilization := new(big.Int).Mul(gasUsed, big.NewInt(10000))
		gasUtilization = gasUtilization.Div(gasUtilization, gasLimit).Div(gasUtilization, big.NewInt(100))

		gu, _ := gasUtilization.Float64()

		blockInfoMap[receipt.BlockNumber] = blockInfo{
			number:         receipt.BlockNumber,
			createdAt:      block.Header.Timestamp,
			numTxs:         len(block.Transactions),
			gasUsed:        new(big.Int).SetUint64(block.Header.GasUsed),
			gasLimit:       new(big.Int).SetUint64(block.Header.GasLimit),
			gasUtilization: gu,
		}

		for _, txn := range block.Transactions {
			txToBlockMap[txn.Hash()] = receipt.BlockNumber
		}
	}

	if len(foundErrors) > 0 {
		fmt.Println("Errors found while waiting for receipts:")

		for _, err := range foundErrors {
			fmt.Println(err)
		}
	}

	return blockInfoMap, txnStats
}

// waitForReceipt waits for the transaction receipt of the given transaction hash.
// It continuously checks for the receipt until it is found or the timeout is reached.
// If the receipt is found, it returns the receipt and nil error.
// If the timeout is reached before the receipt is found, it returns nil receipt and an error.
func (r *BaseLoadTestRunner) waitForReceipt(txHash types.Hash) (*ethgo.Receipt, error) {
	timer := time.NewTimer(r.cfg.ReceiptsTimeout)
	defer timer.Stop()

	ticker := time.NewTicker(time.Second)
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

// calculateTPS calculates the transactions per second (TPS) for a given set of
// block information and transaction statistics.
// It takes a map of block information and an array of transaction statistics as input.
// The function iterates over the transaction statistics and calculates the TPS for each block.
// It also calculates the minimum and maximum TPS values, as well as the total time taken to mine the transactions.
// The calculated TPS values are displayed in a table using the tablewriter package.
// The function returns an error if there is any issue retrieving block information or calculating TPS.
func (r *BaseLoadTestRunner) calculateTPS(blockInfos map[uint64]blockInfo, txnStats []txStats) error {
	fmt.Println("=============================================================")
	fmt.Println("Calculating results...")

	var (
		totalTxs        = len(txnStats)
		totalTime       float64
		maxTxsPerSecond float64
		minTxsPerSecond = math.MaxFloat64
	)

	blockTimeMap := make(map[uint64]uint64)
	uniqueBlocks := map[uint64]struct{}{}

	for _, stat := range txnStats {
		if stat.block == 0 {
			continue
		}

		uniqueBlocks[stat.block] = struct{}{}
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
				blockTimeMap[parentBlockNum] = parentBlockInfo.createdAt
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
				blockTimeMap[block] = currentBlockInfo.createdAt
				currentBlockTxsNum = currentBlockInfo.numTxs
			}
		}

		if currentBlockTxsNum == 0 {
			currentBlockTxsNum = blockInfos[block].numTxs
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

		totalTime += blockTime
	}

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

	infos := make([]blockInfo, 0, len(blockInfos))
	for _, info := range blockInfos {
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].number < infos[j].number
	})

	for _, blockInfo := range infos {
		blockTime := math.Abs(float64(blockInfo.createdAt - blockTimeMap[blockInfo.number-1]))
		tps := float64(blockInfo.numTxs) / blockTime

		table.Append([]string{
			fmt.Sprintf("%d", blockInfo.number),
			fmt.Sprintf("%.2f", blockTime),
			fmt.Sprintf("%d", blockInfo.numTxs),
			fmt.Sprintf("%d", blockInfo.gasUsed.Uint64()),
			fmt.Sprintf("%d", blockInfo.gasLimit.Uint64()),
			fmt.Sprintf("%.2f", blockInfo.gasUtilization),
			fmt.Sprintf("%.2f", tps),
		})
	}

	table.Render()

	table = tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Total Txs", "Total Time To Mine (s)", "Min TPS", "Max TPS", "Average TPS"})
	table.Append([]string{
		fmt.Sprintf("%d", totalTxs),
		fmt.Sprintf("%.2f", totalTime),
		fmt.Sprintf("%.2f", minTxsPerSecond),
		fmt.Sprintf("%.2f", maxTxsPerSecond),
		fmt.Sprintf("%.2f", math.Ceil(float64(totalTxs)/totalTime)),
	})

	table.Render()

	return nil
}

// sendTransactions sends transactions for each virtual user (vu) and returns the transaction hashes.
// It retrieves the chain ID from the client and uses it to send transactions for each user.
// The function runs concurrently for each user using errgroup.
// If the context is canceled, the function returns the context error.
// The transaction hashes are appended to the allTxnHashes slice.
// Finally, the function prints the time taken to send the transactions
// and returns the transaction hashes and nil error.
func (r *BaseLoadTestRunner) sendTransactions(
	sendFn func(*account, *big.Int, *progressbar.ProgressBar) ([]types.Hash, []error, error)) ([]types.Hash, error) {
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

	for _, vu := range r.vus {
		vu := vu

		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()

			default:
				txnHashes, sendErrors, err := sendFn(vu, chainID, bar)
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

		gasPrice := new(big.Int).SetUint64(gp + (gp * 20 / 100))

		feeData.gasPrice = gasPrice
	}

	return feeData, nil
}
