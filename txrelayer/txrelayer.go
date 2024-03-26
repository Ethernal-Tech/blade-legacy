package txrelayer

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/jsonrpc"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	defaultGasPrice            = 1879048192 // 0x70000000
	DefaultGasLimit            = 5242880    // 0x500000
	DefaultRPCAddress          = "http://127.0.0.1:8545"
	defaultNumRetries          = 1000
	gasLimitIncreasePercentage = 100
	feeIncreasePercentage      = 100
	DefaultTimeoutTransactions = 50 * time.Second
	DefaultPollFreq            = 1 * time.Second
)

var (
	errNoAccounts     = errors.New("no accounts registered")
	errMethodNotFound = errors.New("method not found")

	// dynamicFeeTxFallbackErrs represents known errors which are the reason to fallback
	// from sending dynamic fee tx to legacy tx
	dynamicFeeTxFallbackErrs = []error{types.ErrTxTypeNotSupported, errMethodNotFound}
)

type TxRelayer interface {
	// Call executes a message call immediately without creating a transaction on the blockchain
	Call(from types.Address, to types.Address, input []byte) (string, error)
	// SendTransaction signs given transaction by provided key and sends it to the blockchain
	SendTransaction(txn *types.Transaction, key crypto.Key) (*ethgo.Receipt, error)
	// SendTransactionLocal sends non-signed transaction
	// (this function is meant only for testing purposes and is about to be removed at some point)
	SendTransactionLocal(txn *types.Transaction) (*ethgo.Receipt, error)
	// Client returns jsonrpc client
	Client() *jsonrpc.Client
}

var _ TxRelayer = (*TxRelayerImpl)(nil)

type TxRelayerImpl struct {
	ipAddress        string
	client           *jsonrpc.Client
	receiptsPollFreq time.Duration
	receiptsTimeout  time.Duration
	noWaitReceipt    bool

	lock sync.Mutex

	writer io.Writer
}

func NewTxRelayer(opts ...TxRelayerOption) (TxRelayer, error) {
	t := &TxRelayerImpl{
		ipAddress:        DefaultRPCAddress,
		receiptsPollFreq: DefaultPollFreq,
		receiptsTimeout:  DefaultTimeoutTransactions,
	}
	for _, opt := range opts {
		opt(t)
	}

	// Calculate receiptsPollFreq based on receiptsTimeout

	if t.receiptsTimeout >= time.Minute {
		t.receiptsPollFreq = 2 * time.Second
	}

	if t.receiptsPollFreq >= t.receiptsTimeout || t.receiptsTimeout < time.Second {
		// if someone decides to configure a small receipts timeout (in ms)
		// receiptsPollFreq should be less than receiptsTimeout
		t.receiptsPollFreq = t.receiptsTimeout / 2
	}

	if t.client == nil {
		client, err := jsonrpc.NewClient(t.ipAddress)
		if err != nil {
			return nil, err
		}

		t.client = client
	}

	return t, nil
}

// Call executes a message call immediately without creating a transaction on the blockchain
func (t *TxRelayerImpl) Call(from types.Address, to types.Address, input []byte) (string, error) {
	callMsg := &ethgo.CallMsg{
		From: ethgo.Address(from),
		To:   (*ethgo.Address)(&to),
		Data: input,
	}

	return t.client.Eth().Call(callMsg, ethgo.Pending)
}

// SendTransaction signs given transaction by provided key and sends it to the blockchain
func (t *TxRelayerImpl) SendTransaction(txn *types.Transaction, key crypto.Key) (*ethgo.Receipt, error) {
	txnHash, err := t.sendTransactionLocked(txn, key)
	if err != nil {
		if txn.Type() != types.LegacyTxType {
			for _, fallbackErr := range dynamicFeeTxFallbackErrs {
				if strings.Contains(
					strings.ToLower(err.Error()),
					strings.ToLower(fallbackErr.Error())) {
					// "downgrade" transaction to the legacy tx type and resend it
					copyTxn := txn.Copy()
					txn.InitInnerData(types.LegacyTxType)
					txn.SetNonce(copyTxn.Nonce())
					txn.SetInput(copyTxn.Input())
					txn.SetValue(copyTxn.Value())
					txn.SetTo(copyTxn.To())
					txn.SetFrom(copyTxn.From())
					txn.SetGasPrice(big.NewInt(0))

					return t.SendTransaction(txn, key)
				}
			}
		}

		return nil, err
	}

	return t.waitForReceipt(txnHash)
}

// Client returns jsonrpc client
func (t *TxRelayerImpl) Client() *jsonrpc.Client {
	return t.client
}

func (t *TxRelayerImpl) sendTransactionLocked(txn *types.Transaction, key crypto.Key) (ethgo.Hash, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	nonce, err := t.client.Eth().GetNonce(ethgo.Address(key.Address()), ethgo.Pending)
	if err != nil {
		return ethgo.ZeroHash, fmt.Errorf("failed to get nonce: %w", err)
	}

	chainID, err := t.client.Eth().ChainID()
	if err != nil {
		return ethgo.ZeroHash, err
	}

	txn.SetChainID(chainID)
	txn.SetNonce(nonce)

	if txn.From() == types.ZeroAddress {
		txn.SetFrom(key.Address())
	}

	if txn.Type() == types.DynamicFeeTxType {
		maxPriorityFee := txn.GetGasTipCap()
		if maxPriorityFee == nil {
			// retrieve the max priority fee per gas
			if maxPriorityFee, err = t.Client().Eth().MaxPriorityFeePerGas(); err != nil {
				return ethgo.ZeroHash, fmt.Errorf("failed to get max priority fee per gas: %w", err)
			}

			// set retrieved max priority fee per gas increased by certain percentage
			compMaxPriorityFee := new(big.Int).Mul(maxPriorityFee, big.NewInt(feeIncreasePercentage))
			compMaxPriorityFee = compMaxPriorityFee.Div(compMaxPriorityFee, big.NewInt(100))
			txn.SetGasTipCap(new(big.Int).Add(maxPriorityFee, compMaxPriorityFee))
		}

		if txn.GetGasFeeCap() == nil {
			// retrieve the latest base fee
			feeHist, err := t.Client().Eth().FeeHistory(1, ethgo.Latest, nil)
			if err != nil {
				return ethgo.ZeroHash, fmt.Errorf("failed to get fee history: %w", err)
			}

			baseFee := feeHist.BaseFee[len(feeHist.BaseFee)-1]
			// set max fee per gas as sum of base fee and max priority fee
			// (increased by certain percentage)
			maxFeePerGas := new(big.Int).Add(baseFee, maxPriorityFee)
			compMaxFeePerGas := new(big.Int).Mul(maxFeePerGas, big.NewInt(feeIncreasePercentage))
			compMaxFeePerGas = compMaxFeePerGas.Div(compMaxFeePerGas, big.NewInt(100))
			txn.SetGasFeeCap(new(big.Int).Add(maxFeePerGas, compMaxFeePerGas))
		}
	} else if txn.GasPrice() == nil || txn.GasPrice().Uint64() == 0 {
		gasPrice, err := t.Client().Eth().GasPrice()
		if err != nil {
			return ethgo.ZeroHash, fmt.Errorf("failed to get gas price: %w", err)
		}

		gasPriceBigInt := new(big.Int).SetUint64(gasPrice + (gasPrice * feeIncreasePercentage / 100))
		txn.SetGasPrice(gasPriceBigInt)
	}

	if txn.Gas() == 0 {
		gasLimit, err := t.client.Eth().EstimateGas(ConvertTxnToCallMsg(txn))
		if err != nil {
			return ethgo.ZeroHash, fmt.Errorf("failed to estimate gas: %w", err)
		}

		txn.SetGas(gasLimit + (gasLimit * gasLimitIncreasePercentage / 100))
	}

	signer := crypto.NewLondonSigner(
		chainID.Uint64())
	signedTxn, err := signer.SignTxWithCallback(txn,
		func(hash types.Hash) (sig []byte, err error) {
			return key.Sign(hash.Bytes())
		})
	if err != nil {
		return ethgo.ZeroHash, err
	}

	if t.writer != nil {
		var msg string

		if txn.Type() == types.DynamicFeeTxType {
			msg = fmt.Sprintf("[TxRelayer.SendTransaction]\nFrom = %s\nGas = %d\n"+
				"Max Fee Per Gas = %d\nMax Priority Fee Per Gas = %d\n",
				txn.From(), txn.Gas(), txn.GasFeeCap(), txn.GasTipCap())
		} else {
			msg = fmt.Sprintf("[TxRelayer.SendTransaction]\nFrom = %s\nGas = %d\nGas Price = %d\n",
				txn.From(), txn.Gas(), txn.GasPrice())
		}

		_, _ = t.writer.Write([]byte(msg))
	}

	rlpTxn := signedTxn.MarshalRLP()

	return t.client.Eth().SendRawTransaction(rlpTxn)
}

// SendTransactionLocal sends non-signed transaction
// (this function is meant only for testing purposes and is about to be removed at some point)
func (t *TxRelayerImpl) SendTransactionLocal(txn *types.Transaction) (*ethgo.Receipt, error) {
	txnHash, err := t.sendTransactionLocalLocked(txn)
	if err != nil {
		return nil, err
	}

	return t.waitForReceipt(txnHash)
}

func (t *TxRelayerImpl) sendTransactionLocalLocked(txn *types.Transaction) (ethgo.Hash, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	accounts, err := t.client.Eth().Accounts()
	if err != nil {
		return ethgo.ZeroHash, err
	}

	if len(accounts) == 0 {
		return ethgo.ZeroHash, errNoAccounts
	}

	txn.SetFrom(types.Address(accounts[0]))

	gasLimit, err := t.client.Eth().EstimateGas(ConvertTxnToCallMsg(txn))
	if err != nil {
		return ethgo.ZeroHash, err
	}

	txn.SetGas(gasLimit)
	txn.SetGasPrice(new(big.Int).SetUint64(defaultGasPrice))

	return t.client.Eth().SendTransaction(convertTxn(txn))
}

func (t *TxRelayerImpl) waitForReceipt(hash ethgo.Hash) (*ethgo.Receipt, error) {
	if t.noWaitReceipt {
		return nil, nil
	}

	timer := time.NewTimer(t.receiptsTimeout)
	defer timer.Stop()

	ticker := time.NewTicker(t.receiptsPollFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			receipt, err := t.client.Eth().GetTransactionReceipt(hash)
			if err != nil {
				if err.Error() != "not found" {
					return nil, err
				}
			}

			if receipt != nil {
				return receipt, nil
			}
		case <-timer.C:
			return nil, fmt.Errorf("timeout while waiting for transaction %s to be processed", hash)
		}
	}
}

// ConvertTxnToCallMsg converts txn instance to call message
func ConvertTxnToCallMsg(txn *types.Transaction) *ethgo.CallMsg {
	gasPrice := uint64(0)
	if txn.GasPrice() != nil {
		gasPrice = txn.GasPrice().Uint64()
	}

	return &ethgo.CallMsg{
		From:     ethgo.Address(txn.From()),
		To:       (*ethgo.Address)(txn.To()),
		Data:     txn.Input(),
		GasPrice: gasPrice,
		Value:    txn.Value(),
		Gas:      new(big.Int).SetUint64(txn.Gas()),
	}
}

// convertTxn converts transaction from types.Transaction to ethgo.Transaction
func convertTxn(tx *types.Transaction) *ethgo.Transaction {
	getAccessList := func() ethgo.AccessList {
		accessList := make(ethgo.AccessList, 0, len(tx.AccessList()))

		for _, e := range tx.AccessList() {
			storageKeys := make([]ethgo.Hash, 0)

			for _, sk := range e.StorageKeys {
				storageKeys = append(storageKeys, ethgo.Hash(sk))
			}

			accessList = append(accessList,
				ethgo.AccessEntry{
					Address: ethgo.Address(e.Address),
					Storage: storageKeys,
				})
		}

		return accessList
	}

	convertedTx := &ethgo.Transaction{
		From:  ethgo.Address(tx.From()),
		To:    (*ethgo.Address)(tx.To()),
		Input: tx.Input(),
		Value: tx.Value(),
		Gas:   tx.Gas(),
	}

	switch tx.Type() {
	case types.DynamicFeeTxType:
		convertedTx.Type = ethgo.TransactionDynamicFee
		convertedTx.AccessList = getAccessList()
		convertedTx.MaxFeePerGas = tx.GetGasFeeCap()
		convertedTx.MaxPriorityFeePerGas = tx.GetGasTipCap()

		break

	case types.AccessListTxType:
		convertedTx.Type = ethgo.TransactionAccessList
		convertedTx.AccessList = getAccessList()

		break

	default:
		convertedTx.Type = ethgo.TransactionLegacy
		convertedTx.GasPrice = tx.GetGasPrice(0).Uint64()

		break
	}

	return convertedTx
}

type TxRelayerOption func(*TxRelayerImpl)

func WithClient(client *jsonrpc.Client) TxRelayerOption {
	return func(t *TxRelayerImpl) {
		t.client = client
	}
}

func WithIPAddress(ipAddress string) TxRelayerOption {
	return func(t *TxRelayerImpl) {
		t.ipAddress = ipAddress
	}
}

func WithWriter(writer io.Writer) TxRelayerOption {
	return func(t *TxRelayerImpl) {
		t.writer = writer
	}
}

func WithNoWaiting() TxRelayerOption {
	return func(t *TxRelayerImpl) {
		t.noWaitReceipt = true
	}
}

// WithReceiptsTimeout sets the maximum number of eth_getTransactionReceipt retries
// before considering the transaction sending as timed out. Set to -1 to disable
// waitForReceipt and not wait for the transaction receipt
func WithReceiptsTimeout(receiptsTimeout time.Duration) TxRelayerOption {
	return func(t *TxRelayerImpl) {
		t.receiptsTimeout = receiptsTimeout
	}
}
