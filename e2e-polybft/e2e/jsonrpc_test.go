package e2e

import (
	"crypto/tls"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/helper/tests"
	bladeRPC "github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	one = big.NewInt(1)
)

func TestE2E_JsonRPC(t *testing.T) {
	const epochSize = uint64(5)

	preminedAcct, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	cluster := framework.NewTestCluster(t, 4,
		framework.WithEpochSize(int(epochSize)),
		framework.WithPremine(preminedAcct.Address()),
		framework.WithBurnContract(&polybft.BurnContractInfo{BlockNumber: 0, Address: types.ZeroAddress}),
		framework.WithHTTPS(),
		framework.WithTLSCertificate("/etc/ssl/certs/localhost.pem", "/etc/ssl/private/localhost.key"),
	)
	defer cluster.Stop()

	cluster.WaitForReady(t)

	newEthClient, err := bladeRPC.NewEthClient(cluster.Servers[0].JSONRPCAddr())
	require.NoError(t, err)

	t.Run("eth_blockNumber", func(t *testing.T) {
		require.NoError(t, cluster.WaitForBlock(epochSize, 15*time.Second))

		blockNumber, err := newEthClient.BlockNumber()
		require.NoError(t, err)
		require.GreaterOrEqual(t, blockNumber, epochSize)

		require.NoError(t, cluster.WaitForBlock(blockNumber+1, 5*time.Second))

		blockNumber, err = newEthClient.BlockNumber()
		require.NoError(t, err)
		require.GreaterOrEqual(t, blockNumber, epochSize)
	})

	t.Run("eth_getBlock", func(t *testing.T) {
		blockByNumber, err := newEthClient.GetBlockByNumber(bladeRPC.BlockNumber(epochSize), false)
		require.NoError(t, err)
		require.NotNil(t, blockByNumber)
		require.Equal(t, epochSize, blockByNumber.Number())
		require.Empty(t, len(blockByNumber.Transactions)) // since we did not ask for the full block

		blockByNumber, err = newEthClient.GetBlockByNumber(bladeRPC.BlockNumber(epochSize), true)
		require.NoError(t, err)
		require.Equal(t, epochSize, blockByNumber.Number())
		// since we asked for the full block, and epoch ending block has a transaction
		require.Equal(t, 1, len(blockByNumber.Transactions))

		blockByHash, err := newEthClient.GetBlockByHash(blockByNumber.Hash(), false)
		require.NoError(t, err)
		require.NotNil(t, blockByHash)
		require.Equal(t, epochSize, blockByHash.Number())
		require.Equal(t, blockByNumber.Hash(), blockByHash.Hash())

		blockByHash, err = newEthClient.GetBlockByHash(blockByNumber.Hash(), true)
		require.NoError(t, err)
		require.Equal(t, blockByNumber.Hash(), blockByHash.Hash())
		// since we asked for the full block, and epoch ending block has a transaction
		require.Equal(t, 1, len(blockByHash.Transactions))

		// get latest block
		latestBlock, err := newEthClient.GetBlockByNumber(bladeRPC.LatestBlockNumber, false)
		require.NoError(t, err)
		require.NotNil(t, latestBlock)
		require.GreaterOrEqual(t, latestBlock.Number(), epochSize)

		// get pending block
		pendingBlock, err := newEthClient.GetBlockByNumber(bladeRPC.PendingBlockNumber, false)
		require.NoError(t, err)
		require.NotNil(t, pendingBlock)
		require.GreaterOrEqual(t, pendingBlock.Number(), latestBlock.Number())

		// get earliest block
		earliestBlock, err := newEthClient.GetBlockByNumber(bladeRPC.EarliestBlockNumber, false)
		require.NoError(t, err)
		require.NotNil(t, earliestBlock)
		require.Equal(t, uint64(0), earliestBlock.Number())
	})

	t.Run("eth_getCode", func(t *testing.T) {
		deployTxn := cluster.Deploy(t, preminedAcct, contractsapi.TestSimple.Bytecode)
		require.True(t, deployTxn.Succeed())

		target := types.Address(deployTxn.Receipt().ContractAddress)

		code, err := newEthClient.GetCode(target, bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

	t.Run("eth_getStorageAt", func(t *testing.T) {
		key1, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		txn := cluster.Transfer(t, preminedAcct, key1.Address(), ethgo.Ether(1))
		require.True(t, txn.Succeed())

		txn = cluster.Deploy(t, key1, contractsapi.TestSimple.Bytecode)
		require.True(t, txn.Succeed())

		target := types.Address(txn.Receipt().ContractAddress)

		resp, err := newEthClient.GetStorageAt(target, types.Hash{}, bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resp.String())

		setValueFn := contractsapi.TestSimple.Abi.GetMethod("setValue")

		newVal := big.NewInt(1)

		input, err := setValueFn.Encode([]interface{}{newVal})
		require.NoError(t, err)

		txn = cluster.SendTxn(t, key1, types.NewTx(types.NewLegacyTx(types.WithInput(input), types.WithTo(&target))))
		require.True(t, txn.Succeed())

		resp, err = newEthClient.GetStorageAt(target, types.Hash{}, bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resp.String())
	})

	t.Run("eth_getTransactionByHash and eth_getTransactionReceipt", func(t *testing.T) {
		txn := cluster.Transfer(t, preminedAcct, types.StringToAddress("0xDEADBEEF"), one)
		require.True(t, txn.Succeed())

		ethTxn, err := newEthClient.GetTransactionByHash(types.Hash(txn.Receipt().TransactionHash))
		require.NoError(t, err)

		require.Equal(t, ethTxn.From(), preminedAcct.Address())

		receipt, err := newEthClient.GetTransactionReceipt(ethTxn.Hash())
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, ethTxn.Hash(), types.Hash(receipt.TransactionHash))
	})

	t.Run("eth_getTransactionCount", func(t *testing.T) {
		nonce, err := newEthClient.GetNonce(preminedAcct.Address(), bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.GreaterOrEqual(t, nonce, uint64(0)) // since we used this account in previous tests

		txn := cluster.Transfer(t, preminedAcct, types.StringToAddress("0xDEADBEEF"), one)
		require.True(t, txn.Succeed())

		newNonce, err := newEthClient.GetNonce(preminedAcct.Address(), bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.Equal(t, nonce+1, newNonce)
	})

	t.Run("eth_getBalance", func(t *testing.T) {
		balance, err := newEthClient.GetBalance(preminedAcct.Address(), bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.True(t, balance.Cmp(big.NewInt(0)) >= 0)

		receiver := types.StringToAddress("0xDEADFFFF")

		tokens := ethgo.Ether(1)

		txn := cluster.Transfer(t, preminedAcct, receiver, tokens)
		require.True(t, txn.Succeed())

		newBalance, err := newEthClient.GetBalance(receiver, bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.Equal(t, tokens, newBalance)
	})

	t.Run("eth_estimateGas", func(t *testing.T) {
		deployTxn := cluster.Deploy(t, preminedAcct, contractsapi.TestSimple.Bytecode)
		require.True(t, deployTxn.Succeed())

		target := types.Address(deployTxn.Receipt().ContractAddress)
		input := contractsapi.TestSimple.Abi.GetMethod("getValue").ID()

		estimatedGas, err := newEthClient.EstimateGas(&bladeRPC.CallMsg{
			From: preminedAcct.Address(),
			To:   &target,
			Data: input,
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, estimatedGas, uint64(0))
	})

	t.Run("eth_gasPrice", func(t *testing.T) {
		gasPrice, err := newEthClient.GasPrice()
		require.NoError(t, err)
		require.Greater(t, gasPrice, uint64(0)) // london fork is enabled, so gas price should be greater than 0
	})

	t.Run("eth_call", func(t *testing.T) {
		deployTxn := cluster.Deploy(t, preminedAcct, contractsapi.TestSimple.Bytecode)
		require.True(t, deployTxn.Succeed())

		target := types.Address(deployTxn.Receipt().ContractAddress)
		input := contractsapi.TestSimple.Abi.GetMethod("getValue").ID()

		acctZeroBalance, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		resp, err := newEthClient.Call(&bladeRPC.CallMsg{
			From: types.Address(acctZeroBalance.Address()),
			To:   &target,
			Data: input,
		}, bladeRPC.LatestBlockNumber, nil)
		require.NoError(t, err)
		require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resp)
	})

	t.Run("eth_chainID", func(t *testing.T) {
		chainID, err := newEthClient.ChainID()
		require.NoError(t, err)
		require.Equal(t, big.NewInt(100), chainID) // default chainID
	})

	t.Run("eth_maxPriorityFeePerGas", func(t *testing.T) {
		maxPriorityFeePerGas, err := newEthClient.MaxPriorityFeePerGas()
		require.NoError(t, err)
		// london fork is enabled, so maxPriorityFeePerGas should be greater than 0
		require.True(t, maxPriorityFeePerGas.Cmp(big.NewInt(0)) > 0)
	})

	t.Run("eth_sendRawTransaction", func(t *testing.T) {
		receiver := types.StringToAddress("0xDEADFFFF")
		tokenAmount := ethgo.Ether(1)

		chainID, err := newEthClient.ChainID()
		require.NoError(t, err)

		gasPrice, err := newEthClient.GasPrice()
		require.NoError(t, err)

		newAccountKey, newAccountAddr := tests.GenerateKeyAndAddr(t)

		transferTxn := cluster.Transfer(t, preminedAcct, newAccountAddr, tokenAmount)
		require.True(t, transferTxn.Succeed())

		newAccountBalance, err := newEthClient.GetBalance(newAccountAddr, bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.Equal(t, tokenAmount, newAccountBalance)

		txn := types.NewTx(
			types.NewLegacyTx(
				types.WithNonce(0),
				types.WithFrom(newAccountAddr),
				types.WithTo(&receiver),
				types.WithValue(ethgo.Gwei(1)),
				types.WithGas(21000),
				types.WithGasPrice(new(big.Int).SetUint64(gasPrice)),
			))

		signedTxn, err := crypto.NewLondonSigner(chainID.Uint64()).SignTx(txn, newAccountKey)
		require.NoError(t, err)

		data := signedTxn.MarshalRLPTo(nil)

		hash, err := newEthClient.SendRawTransaction(data)
		require.NoError(t, err)
		require.NotEqual(t, types.ZeroHash, hash)
	})

	t.Run("eth_sendTransaction", func(t *testing.T) {
		deployTxn := cluster.Deploy(t, preminedAcct, contractsapi.TestSimple.Bytecode)
		require.True(t, deployTxn.Succeed())

		newNonce, err := newEthClient.GetNonce(preminedAcct.Address(), bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)

		target := types.Address(deployTxn.Receipt().ContractAddress)
		input := contractsapi.TestSimple.Abi.GetMethod("getValue").ID()

		gasPrice, err := newEthClient.GasPrice()
		require.NoError(t, err)

		txn := &bladeRPC.CallMsg{
			From:     types.ZeroAddress,
			To:       &target,
			Gas:      92100,
			GasPrice: new(big.Int).SetUint64(gasPrice),
			Nonce:    newNonce,
			Data:     input,
		}

		hash, err := newEthClient.SendTransactionCallMsg(txn)
		require.NoError(t, err)
		require.NotEqual(t, types.ZeroHash, hash)
	})

	t.Run("eth_signTransaction", func(t *testing.T) {
		deployTxn := cluster.Deploy(t, preminedAcct, contractsapi.TestSimple.Bytecode)
		require.True(t, deployTxn.Succeed())

		target := types.Address(deployTxn.Receipt().ContractAddress)
		input := contractsapi.TestSimple.Abi.GetMethod("getValue").ID()

		zeroAddress := types.ZeroAddress

		newNonce, err := newEthClient.GetNonce(target, bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)

		chainID, err := newEthClient.ChainID()
		require.NoError(t, err)

		gasPrice, err := newEthClient.GasPrice()
		require.NoError(t, err)

		txn := &bladeRPC.CallMsg{
			From:     zeroAddress,
			To:       &target,
			Gas:      2100,
			GasPrice: new(big.Int).SetUint64(gasPrice),
			//MaxPriorityFeePerGas: new(big.Int).SetUint64(0),
			//MaxFeePerGas:         new(big.Int).SetUint64(10),
			Nonce:   newNonce,
			Data:    input,
			ChainID: chainID,
		}

		res, err := newEthClient.SignTransaction(txn)
		require.NoError(t, err)
		require.NotEqual(t, types.ZeroHash, res.Tx.From)
		require.NotNil(t, res.Raw)
		require.NotEqual(t, 0, len(*res.Raw))
	})

	t.Run("eth_sign", func(t *testing.T) {
		receiver := types.StringToAddress("0xDEADFFFF")
		tokenAmount := ethgo.Ether(1)

		chainID, err := newEthClient.ChainID()
		require.NoError(t, err)

		gasPrice, err := newEthClient.GasPrice()
		require.NoError(t, err)

		newAccountKey, newAccountAddr := tests.GenerateKeyAndAddr(t)

		transferTxn := cluster.Transfer(t, preminedAcct, newAccountAddr, tokenAmount)
		require.True(t, transferTxn.Succeed())

		newAccountBalance, err := newEthClient.GetBalance(newAccountAddr, bladeRPC.LatestBlockNumberOrHash)
		require.NoError(t, err)
		require.Equal(t, tokenAmount, newAccountBalance)

		txn := types.NewTx(
			types.NewLegacyTx(
				types.WithNonce(0),
				types.WithFrom(newAccountAddr),
				types.WithTo(&receiver),
				types.WithValue(ethgo.Gwei(1)),
				types.WithGas(21000),
				types.WithGasPrice(new(big.Int).SetUint64(gasPrice)),
			))

		signedTxn, err := crypto.NewLondonSigner(chainID.Uint64()).SignTx(txn, newAccountKey)
		require.NoError(t, err)

		data := signedTxn.MarshalRLPTo(nil)

		dataSign, err := newEthClient.Sign(preminedAcct.Address(), data)
		require.NoError(t, err)
		require.NotEqual(t, 0, dataSign[64])
	})

	t.Run("eth_getHeaderByNumber", func(t *testing.T) {
		key1, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		txn := cluster.Transfer(t, preminedAcct, key1.Address(), one)
		require.True(t, txn.Succeed())
		txReceipt := txn.Receipt()

		var header types.Header
		err = newEthClient.EndpointCall("eth_getHeaderByNumber", &header, ethgo.BlockNumber(txReceipt.BlockNumber))
		require.NoError(t, err)

		require.Equal(t, txReceipt.BlockNumber, header.Number)
		require.Equal(t, txReceipt.BlockHash, ethgo.Hash(header.Hash))
	})

	t.Run("eth_getHeaderByHash", func(t *testing.T) {
		key1, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		txn := cluster.Transfer(t, preminedAcct, key1.Address(), one)
		require.True(t, txn.Succeed())
		txReceipt := txn.Receipt()

		var header types.Header
		err = newEthClient.EndpointCall("eth_getHeaderByHash", &header, txReceipt.BlockHash)
		require.NoError(t, err)

		require.Equal(t, txReceipt.BlockNumber, header.Number)
		require.Equal(t, txReceipt.BlockHash, ethgo.Hash(header.Hash))
	})
}

func TestE2E_JsonRPCSelfSignedTLS(t *testing.T) {
	cluster := framework.NewTestCluster(t, 4,
		framework.WithHTTPS(),
	)
	defer cluster.Stop()

	// Wait for endpoint to start, can't use cluster.WaitForReady because server certificate is not trusted by client
	time.Sleep(1 * time.Second)

	addr := cluster.Servers[0].JSONRPCAddr()
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	_, err := client.Get(addr)
	require.NoError(t, err)

	// This will fail with certificate signed by unknown authority error
	client = &http.Client{}
	_, err = client.Get(addr)
	require.Error(t, err)
	require.ErrorContains(t, err, "x509: certificate signed by unknown authority")
}
