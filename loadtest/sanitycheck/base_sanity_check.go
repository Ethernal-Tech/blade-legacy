package sanitycheck

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

// SanityCheckTest represents a sanity check test.
type SanityCheckTest interface {
	Name() string
	Run() error
}

// BaseSanityCheckTest represents a base sanity check test
type BaseSanityCheckTest struct {
	config *SanityCheckTestConfig

	testAccountKey *crypto.ECDSAKey
	client         *jsonrpc.EthClient

	txrelayer txrelayer.TxRelayer
}

// NewBaseSanityCheckTest creates a new BaseSanityCheckTest
func NewBaseSanityCheckTest(cfg *SanityCheckTestConfig,
	testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) (*BaseSanityCheckTest, error) {
	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithClient(client),
		txrelayer.WithReceiptsTimeout(cfg.ReceiptsTimeout),
	)
	if err != nil {
		return nil, err
	}

	return &BaseSanityCheckTest{
		config:         cfg,
		client:         client,
		txrelayer:      txRelayer,
		testAccountKey: testAccountKey,
	}, nil
}

// decodePrivateKey decodes the given private key string.
func (t *BaseSanityCheckTest) decodePrivateKey(privateKeyRaw string) (*crypto.ECDSAKey, error) {
	raw, err := hex.DecodeString(privateKeyRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key string '%s': %w", privateKeyRaw, err)
	}

	return crypto.NewECDSAKeyFromRawPrivECDSA(raw)
}

// fundAddress funds native tokens to the given address.
func (t *BaseSanityCheckTest) fundAddress(address types.Address, amount *big.Int) error {
	fmt.Println("Funding address", address.String(), "Amount", amount.String())

	s := time.Now().UTC()
	defer func() {
		fmt.Println("Funding address", address.String(), "took", time.Since(s))
	}()

	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithClient(t.client),
		txrelayer.WithReceiptsTimeout(t.config.ReceiptsTimeout),
	)
	if err != nil {
		return err
	}

	tx := types.NewTx(types.NewLegacyTx(
		types.WithTo(&address),
		types.WithFrom(t.testAccountKey.Address()),
		types.WithValue(amount),
		types.WithGas(21000),
	))

	receipt, err := txRelayer.SendTransaction(tx, t.testAccountKey)
	if err != nil {
		return err
	}

	if receipt == nil || receipt.Status != uint64(types.ReceiptSuccess) {
		return fmt.Errorf("failed to fund native tokens to %s", address)
	}

	return nil
}

// CreateApproveERC20Txn sends approve transaction
// to ERC20 token for spender so that it is able to spend given tokens
func (t *BaseSanityCheckTest) approveNativeERC20(sender *crypto.ECDSAKey,
	amount *big.Int, spender types.Address) error {
	fmt.Println("Approving", amount.String(), "tokens for", spender.String())

	s := time.Now().UTC()
	defer func() {
		fmt.Println("Approving", amount.String(), "for", spender.String(), "took", time.Since(s))
	}()

	approveFnParams := &contractsapi.ApproveRootERC20Fn{
		Spender: spender,
		Amount:  amount,
	}

	input, err := approveFnParams.EncodeAbi()
	if err != nil {
		return fmt.Errorf("failed to encode parameters for approve erc20 transaction. error: %w", err)
	}

	tx := types.NewTx(types.NewDynamicFeeTx(
		types.WithFrom(types.ZeroAddress),
		types.WithTo(&contracts.NativeERC20TokenContract),
		types.WithInput(input)))

	receipt, err := t.txrelayer.SendTransaction(tx, sender)
	if err != nil {
		return err
	}

	if receipt.Status != uint64(types.ReceiptSuccess) {
		return fmt.Errorf("approve transaction failed on block %d", receipt.BlockNumber)
	}

	return nil
}

// waitForEndOfEpoch waits for the end of the current epoch.
func (t *BaseSanityCheckTest) waitForEpochEnding() (*types.Header, error) {
	fmt.Println("Waiting for end of epoch")

	currentBlock, err := t.client.GetBlockByNumber(jsonrpc.LatestBlockNumber, false)
	if err != nil {
		return nil, err
	}

	timer := time.NewTimer(2 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)

	defer func() {
		timer.Stop()
		ticker.Stop()
		fmt.Println("Waiting for end of epoch finished")
	}()

	for {
		select {
		case <-timer.C:
			return nil, fmt.Errorf("timed out waiting for end of epoch")
		case <-ticker.C:
			if currentBlock.Number()%t.config.EpochSize == 0 {
				return currentBlock.Header, nil
			}

			block, err := t.client.GetBlockByNumber(jsonrpc.BlockNumber(currentBlock.Number()+1), false)
			if err != nil {
				return nil, err
			}

			if block == nil {
				continue
			}

			currentBlock = block
		}
	}
}
