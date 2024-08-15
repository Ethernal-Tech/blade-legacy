package framework

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/Ethernal-Tech/ethgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	empty "google.golang.org/protobuf/types/known/emptypb"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/genesis"
	"github.com/0xPolygon/polygon-edge/command/server"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/tests"
	"github.com/0xPolygon/polygon-edge/server/proto"
	txpoolProto "github.com/0xPolygon/polygon-edge/txpool/proto"
	"github.com/0xPolygon/polygon-edge/types"
)

type TestServerConfigCallback func(*TestServerConfig)

const (
	serverIP    = "127.0.0.1"
	initialPort = 12000
	binaryName  = "polygon-edge"
)

type TestServer struct {
	t *testing.T

	Config *TestServerConfig
	cmd    *exec.Cmd
}

func NewTestServer(t *testing.T, rootDir string, callback TestServerConfigCallback) *TestServer {
	t.Helper()

	// Reserve ports
	ports, err := FindAvailablePorts(3, initialPort, initialPort+10000)
	if err != nil {
		t.Fatal(err)
	}

	// Sets the services to start on open ports
	config := &TestServerConfig{
		ReservedPorts: ports,
		GRPCPort:      ports[0].Port(),
		LibP2PPort:    ports[1].Port(),
		JSONRPCPort:   ports[2].Port(),
		RootDir:       rootDir,
		Signer:        crypto.NewSigner(chain.AllForksEnabled.At(0), 100),
	}

	if callback != nil {
		callback(config)
	}

	return &TestServer{
		t:      t,
		Config: config,
	}
}

func (t *TestServer) GrpcAddr() string {
	return fmt.Sprintf("%s:%d", serverIP, t.Config.GRPCPort)
}

func (t *TestServer) LibP2PAddr() string {
	return fmt.Sprintf("%s:%d", serverIP, t.Config.LibP2PPort)
}

func (t *TestServer) JSONRPCAddr() string {
	return fmt.Sprintf("%s:%d", serverIP, t.Config.JSONRPCPort)
}

func (t *TestServer) HTTPJSONRPCURL() string {
	return fmt.Sprintf("http://%s", t.JSONRPCAddr())
}

func (t *TestServer) WSJSONRPCURL() string {
	return fmt.Sprintf("ws://%s/ws", t.JSONRPCAddr())
}

func (t *TestServer) JSONRPC() *jsonrpc.EthClient {
	clt, err := jsonrpc.NewEthClient(t.HTTPJSONRPCURL())
	if err != nil {
		t.t.Fatal(err)
	}

	return clt
}

func (t *TestServer) Operator() proto.SystemClient {
	conn, err := grpc.Dial(
		t.GrpcAddr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.t.Fatal(err)
	}

	return proto.NewSystemClient(conn)
}

func (t *TestServer) TxnPoolOperator() txpoolProto.TxnPoolOperatorClient {
	conn, err := grpc.Dial(
		t.GrpcAddr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.t.Fatal(err)
	}

	return txpoolProto.NewTxnPoolOperatorClient(conn)
}

func (t *TestServer) ReleaseReservedPorts() {
	for _, p := range t.Config.ReservedPorts {
		if err := p.Close(); err != nil {
			t.t.Error(err)
		}
	}

	t.Config.ReservedPorts = nil
}

func (t *TestServer) Stop() {
	t.ReleaseReservedPorts()

	if t.cmd != nil {
		if err := t.cmd.Process.Kill(); err != nil {
			t.t.Error(err)
		}
	}
}

func (t *TestServer) GetLatestBlockHeight() (uint64, error) {
	return t.JSONRPC().BlockNumber()
}

type InitIBFTResult struct {
	Address string
	NodeID  string
}

func (t *TestServer) GenerateGenesis() error {
	genesisCmd := genesis.GetCommand()
	args := []string{
		genesisCmd.Use,
	}

	// add pre-mined accounts
	for _, acct := range t.Config.PremineAccts {
		args = append(args, "--premine", acct.Addr.String()+":0x"+acct.Balance.Text(16))
	}

	// add consensus flags
	switch t.Config.Consensus {
	case ConsensusDev:
		args = append(args, "--consensus", "dev")
	case ConsensusDummy:
		args = append(args, "--consensus", "dummy")
	}

	for _, bootnode := range t.Config.Bootnodes {
		args = append(args, "--bootnode", bootnode)
	}

	// add block gas limit
	if t.Config.BlockGasLimit == 0 {
		t.Config.BlockGasLimit = command.DefaultGenesisGasLimit
	}

	blockGasLimit := strconv.FormatUint(t.Config.BlockGasLimit, 10)
	args = append(args, "--block-gas-limit", blockGasLimit)

	args = append(args, "--burn-contract", fmt.Sprintf("0:%s:%s",
		t.Config.BurnContractAddr, types.ZeroAddress))

	cmd := exec.Command(resolveBinary(), args...) //nolint:gosec
	cmd.Dir = t.Config.RootDir

	stdout := t.GetStdout()
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	return cmd.Run()
}

func (t *TestServer) Start(ctx context.Context) error {
	serverCmd := server.GetCommand()
	args := []string{
		serverCmd.Use,
		// add custom chain
		"--chain", filepath.Join(t.Config.RootDir, "genesis.json"),
		// enable grpc
		"--grpc-address", t.GrpcAddr(),
		// enable libp2p
		"--libp2p", t.LibP2PAddr(),
		// enable jsonrpc
		"--jsonrpc", t.JSONRPCAddr(),
	}

	switch t.Config.Consensus {
	case ConsensusDev:
		args = append(args, "--data-dir", t.Config.RootDir)
		args = append(args, "--dev")

		if t.Config.DevInterval != 0 {
			args = append(args, "--dev-interval", strconv.Itoa(t.Config.DevInterval))
		}
	case ConsensusDummy:
		args = append(args, "--data-dir", t.Config.RootDir)
	}

	if t.Config.PriceLimit != nil {
		args = append(args, "--price-limit", strconv.FormatUint(*t.Config.PriceLimit, 10))
	}

	if t.Config.ShowsLog || t.Config.SaveLogs {
		args = append(args, "--log-level", "debug")
	}

	// add block gas target
	if t.Config.BlockGasTarget != 0 {
		args = append(args, "--block-gas-target", *common.EncodeUint64(t.Config.BlockGasTarget))
	}

	t.ReleaseReservedPorts()

	// Start the server
	t.cmd = exec.Command(resolveBinary(), args...) //nolint:gosec
	t.cmd.Dir = t.Config.RootDir

	stdout := t.GetStdout()
	t.cmd.Stdout = stdout
	t.cmd.Stderr = stdout

	if err := t.cmd.Start(); err != nil {
		return err
	}

	_, err := tests.RetryUntilTimeout(ctx, func() (interface{}, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := t.Operator().GetStatus(ctx, &empty.Empty{}); err != nil {
			return nil, true
		}

		return nil, false
	})
	if err != nil {
		return err
	}

	return nil
}

// SignTx is a helper method for signing transactions
func (t *TestServer) SignTx(
	transaction *types.Transaction,
	privateKey *ecdsa.PrivateKey,
) (*types.Transaction, error) {
	return t.Config.Signer.SignTx(transaction, privateKey)
}

// DeployContract deploys a contract with account 0 and returns the address
func (t *TestServer) DeployContract(
	ctx context.Context,
	binary string,
	privateKey *ecdsa.PrivateKey,
) (types.Address, error) {
	buf, err := hex.DecodeString(binary)
	if err != nil {
		return types.ZeroAddress, err
	}

	sender, err := crypto.GetAddressFromKey(privateKey)
	if err != nil {
		return types.ZeroAddress, fmt.Errorf("unable to extract key, %w", err)
	}

	receipt, err := t.SendRawTx(ctx, &PreparedTransaction{
		From:     sender,
		Gas:      DefaultGasLimit,
		GasPrice: big.NewInt(DefaultGasPrice),
		Input:    buf,
	}, privateKey)
	if err != nil {
		return types.ZeroAddress, err
	}

	return types.Address(receipt.ContractAddress), nil
}

const (
	DefaultGasPrice = 10e9    // 0x2540BE400
	DefaultGasLimit = 5242880 // 0x500000
)

type PreparedTransaction struct {
	From     types.Address
	GasPrice *big.Int
	Gas      uint64
	To       *types.Address
	Value    *big.Int
	Input    []byte
}

// SendRawTx signs the transaction with the provided private key, executes it, and returns the receipt
func (t *TestServer) SendRawTx(
	ctx context.Context,
	tx *PreparedTransaction,
	signerKey *ecdsa.PrivateKey,
) (*ethgo.Receipt, error) {
	client := t.JSONRPC()

	nextNonce, err := client.GetNonce(tx.From, jsonrpc.LatestBlockNumberOrHash)
	if err != nil {
		return nil, err
	}

	signedTx, err := t.SignTx(types.NewTx(types.NewLegacyTx(
		types.WithGasPrice(tx.GasPrice),
		types.WithGas(tx.Gas),
		types.WithTo(tx.To),
		types.WithValue(tx.Value),
		types.WithInput(tx.Input),
		types.WithNonce(nextNonce),
		types.WithFrom(tx.From),
	)), signerKey)
	if err != nil {
		return nil, err
	}

	txHash, err := client.SendRawTransaction(signedTx.MarshalRLP())
	if err != nil {
		return nil, err
	}

	return tests.WaitForReceipt(ctx, t.JSONRPC(), txHash)
}

func (t *TestServer) WaitForReceipt(ctx context.Context, hash types.Hash) (*ethgo.Receipt, error) {
	client := t.JSONRPC()

	type result struct {
		receipt *ethgo.Receipt
		err     error
	}

	res, err := tests.RetryUntilTimeout(ctx, func() (interface{}, bool) {
		receipt, err := client.GetTransactionReceipt(hash)

		if err != nil && err.Error() != "not found" {
			return result{receipt, err}, false
		}

		if receipt != nil {
			return result{receipt, nil}, false
		}

		return nil, true
	})
	if err != nil {
		return nil, err
	}

	data, ok := res.(result)
	if !ok {
		return nil, errors.New("invalid type assertion")
	}

	return data.receipt, data.err
}

// GetGasTotal waits for the total gas used sum for the passed in
// transactions
func (t *TestServer) GetGasTotal(txHashes []types.Hash) uint64 {
	t.t.Helper()

	var (
		totalGasUsed    = uint64(0)
		receiptErrs     = make([]error, 0)
		receiptErrsLock sync.Mutex
		wg              sync.WaitGroup
	)

	appendReceiptErr := func(receiptErr error) {
		receiptErrsLock.Lock()
		defer receiptErrsLock.Unlock()

		receiptErrs = append(receiptErrs, receiptErr)
	}

	for _, txHash := range txHashes {
		wg.Add(1)

		go func(txHash types.Hash) {
			defer wg.Done()

			ctx, cancelFn := context.WithTimeout(context.Background(), DefaultTimeout)
			defer cancelFn()

			receipt, receiptErr := tests.WaitForReceipt(ctx, t.JSONRPC(), txHash)
			if receiptErr != nil {
				appendReceiptErr(fmt.Errorf("unable to wait for receipt, %w", receiptErr))

				return
			}

			atomic.AddUint64(&totalGasUsed, receipt.GasUsed)
		}(txHash)
	}

	wg.Wait()

	if len(receiptErrs) > 0 {
		t.t.Fatalf("unable to wait for receipts, %v", receiptErrs)
	}

	return totalGasUsed
}

func (t *TestServer) WaitForReady(ctx context.Context) error {
	_, err := tests.RetryUntilTimeout(ctx, func() (interface{}, bool) {
		num, err := t.GetLatestBlockHeight()

		if num < 1 || err != nil {
			return nil, true
		}

		return nil, false
	})

	return err
}

func (t *TestServer) InvokeMethod(
	ctx context.Context,
	contractAddress types.Address,
	method string,
	fromKey *ecdsa.PrivateKey,
) *ethgo.Receipt {
	sig := MethodSig(method)

	fromAddress, err := crypto.GetAddressFromKey(fromKey)
	if err != nil {
		t.t.Fatalf("unable to extract key, %v", err)
	}

	receipt, err := t.SendRawTx(ctx, &PreparedTransaction{
		Gas:      DefaultGasLimit,
		GasPrice: big.NewInt(DefaultGasPrice),
		To:       &contractAddress,
		From:     fromAddress,
		Input:    sig,
	}, fromKey)

	if err != nil {
		t.t.Fatal(err)
	}

	return receipt
}

func (t *TestServer) CallJSONRPC(req map[string]interface{}) map[string]interface{} {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		t.t.Fatal(err)

		return nil
	}

	url := fmt.Sprintf("http://%s", t.JSONRPCAddr())

	//nolint:gosec // this is not used because it can't be defined as a global variable
	response, err := http.Post(url, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.t.Fatalf("failed to send request to JSON-RPC server: %v", err)

		return nil
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.t.Fatalf("JSON-RPC doesn't return ok: %s", response.Status)

		return nil
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.t.Fatalf("failed to read HTTP body: %s", err)

		return nil
	}

	result := map[string]interface{}{}

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		t.t.Fatalf("failed to convert json to object: %s", err)

		return nil
	}

	return result
}

// GetStdout returns the combined stdout writers of the server
func (t *TestServer) GetStdout() io.Writer {
	writers := []io.Writer{}

	if t.Config.SaveLogs {
		f, err := os.OpenFile(filepath.Join(t.Config.LogsDir, t.Config.Name+".log"), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
		if err != nil {
			t.t.Fatal(err)
		}

		writers = append(writers, f)

		t.t.Cleanup(func() {
			err = f.Close()
			if err != nil {
				t.t.Logf("Failed to close file. Error: %s", err)
			}
		})
	}

	if t.Config.ShowsLog {
		writers = append(writers, os.Stdout)
	}

	if len(writers) == 0 {
		return io.Discard
	}

	return io.MultiWriter(writers...)
}

func resolveBinary() string {
	bin := os.Getenv("EDGE_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return binaryName
}
