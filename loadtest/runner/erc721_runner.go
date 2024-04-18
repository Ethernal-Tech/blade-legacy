package runner

import (
	"fmt"
	"math/big"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

const nftURL = "https://really-valuable-nft-page.io"

// ERC721Runner represents a load test runner for ERC721 tokens.
type ERC721Runner struct {
	*BaseLoadTestRunner

	erc721Token         types.Address
	erc721TokenArtifact *contracts.Artifact
	txInput             []byte
}

// NewERC721Runner creates a new ERC721Runner instance with the given LoadTestConfig.
// It returns a pointer to the created ERC721Runner and an error, if any.
func NewERC721Runner(cfg LoadTestConfig) (*ERC721Runner, error) {
	runner, err := NewBaseLoadTestRunner(cfg)
	if err != nil {
		return nil, err
	}

	return &ERC721Runner{BaseLoadTestRunner: runner}, nil
}

// Run executes the ERC20 load test.
// It performs the following steps:
// 1. Creates virtual users (VUs).
// 2. Funds the VUs with native tokens.
// 3. Deploys the ERC721 token contract.
// 4. Sends NFT transactions using the VUs.
// 5. Waits for the transaction pool to empty.
// 6. Waits for transaction receipts.
// 7. Calculates the transactions per second (TPS) based on block information and transaction statistics.
// Returns an error if any of the steps fail.
func (e *ERC721Runner) Run() error {
	fmt.Println("Running ERC721 load test", e.cfg.LoadTestName)

	if err := e.createVUs(); err != nil {
		return err
	}

	if err := e.fundVUs(); err != nil {
		return err
	}

	if err := e.deployERC21Token(); err != nil {
		return err
	}

	if !e.cfg.WaitForTxPoolToEmpty {
		go e.waitForReceiptsParallel()
		go e.calculateResultsParallel()

		_, err := e.sendTransactions(e.createERC721Transaction)
		if err != nil {
			return err
		}

		return <-e.done
	}

	txHashes, err := e.sendTransactions(e.createERC721Transaction)
	if err != nil {
		return err
	}

	if err := e.waitForTxPoolToEmpty(); err != nil {
		return err
	}

	return e.calculateResults(e.waitForReceipts(txHashes))
}

// deployERC21Token deploys an ERC721 token contract.
// It loads the contract artifact from the specified file path,
// encodes the constructor inputs, creates a new transaction,
// sends the transaction using a transaction relayer,
// and retrieves the deployment receipt.
// If the deployment is successful, it sets the ERC721 token address
// and artifact in the ERC721Runner instance.
// Returns an error if any step of the deployment process fails.
func (e *ERC721Runner) deployERC21Token() error {
	fmt.Println("=============================================================")
	fmt.Println("Deploying ERC721 token contract")

	start := time.Now().UTC()
	artifact := contractsapi.ZexNFT

	input, err := artifact.Abi.Constructor.Inputs.Encode(map[string]interface{}{
		"tokenName":   "ZexCoin",
		"tokenSymbol": "ZEX",
	})

	if err != nil {
		return err
	}

	txn := types.NewTx(types.NewLegacyTx(
		types.WithTo(nil),
		types.WithInput(append(artifact.Bytecode, input...)),
		types.WithFrom(e.loadTestAccount.key.Address()),
	))

	txRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(e.client))
	if err != nil {
		return err
	}

	receipt, err := txRelayer.SendTransaction(txn, e.loadTestAccount.key)
	if err != nil {
		return err
	}

	if receipt == nil || receipt.Status == uint64(types.ReceiptFailed) {
		return fmt.Errorf("failed to deploy ERC721 token")
	}

	e.erc721Token = types.Address(receipt.ContractAddress)
	e.erc721TokenArtifact = artifact

	input, err = e.erc721TokenArtifact.Abi.Methods["createNFT"].Encode(map[string]interface{}{"tokenURI": nftURL})
	if err != nil {
		return err
	}

	e.txInput = input

	fmt.Printf("Deploying ERC721 token took %s\n", time.Since(start))

	return nil
}

// createERC721Transaction creates an ERC721 transaction
func (e *ERC721Runner) createERC721Transaction(account *account, feeData *feeData,
	chainID *big.Int) *types.Transaction {
	if e.cfg.DynamicTxs {
		return types.NewTx(types.NewDynamicFeeTx(
			types.WithNonce(account.nonce),
			types.WithTo(&e.erc721Token),
			types.WithFrom(account.key.Address()),
			types.WithGasFeeCap(feeData.gasFeeCap),
			types.WithGasTipCap(feeData.gasTipCap),
			types.WithChainID(chainID),
			types.WithInput(e.txInput),
		))
	}

	return types.NewTx(types.NewLegacyTx(
		types.WithNonce(account.nonce),
		types.WithTo(&e.erc721Token),
		types.WithGasPrice(feeData.gasPrice),
		types.WithFrom(account.key.Address()),
		types.WithInput(e.txInput),
	))
}
