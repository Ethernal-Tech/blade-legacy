package e2e

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/wallet"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/types"
)

func TestE2E_Storage(t *testing.T) {
	sender, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	cluster := framework.NewTestCluster(t, 5,
		framework.WithPremine(sender.Address()),
		framework.WithBurnContract(&polybft.BurnContractInfo{BlockNumber: 0, Address: types.ZeroAddress}),
	)
	defer cluster.Stop()

	cluster.WaitForReady(t)

	client := cluster.Servers[0].JSONRPC()

	num := 20

	receivers := []types.Address{}

	for i := 0; i < num; i++ {
		key, err := wallet.GenerateKey()
		require.NoError(t, err)

		receivers = append(receivers, types.Address(key.Address()))
	}

	txs := []*framework.TestTxn{}

	for i := 0; i < num; i++ {
		func(i int, to types.Address) {
			// Send every second transaction as a dynamic fees one
			var txn *types.Transaction

			if i%2 == 0 {
				chainID, err := client.ChainID()
				require.NoError(t, err)

				txn = types.NewTx(types.NewDynamicFeeTx(
					types.WithGasFeeCap(big.NewInt(1000000000)),
					types.WithGasTipCap(big.NewInt(100000000)),
					types.WithChainID(chainID),
				))
			} else {
				txn = types.NewTx(types.NewLegacyTx(
					types.WithGasPrice(ethgo.Gwei(2)),
				))
			}

			txn.SetFrom(sender.Address())
			txn.SetTo((*types.Address)(&to))
			txn.SetGas(21000)
			txn.SetValue(big.NewInt(int64(i)))
			txn.SetNonce(uint64(i))

			tx := cluster.SendTxn(t, sender, txn)
			require.True(t, tx.Succeed())

			txs = append(txs, tx)
		}(i, receivers[i])
	}

	err = cluster.WaitUntil(2*time.Minute, 2*time.Second, func() bool {
		for i, receiver := range receivers {
			balance, err := client.GetBalance(receiver, jsonrpc.LatestBlockNumberOrHash)
			if err != nil {
				return true
			}

			t.Logf("Balance %s %s", receiver, balance)

			if balance.Uint64() != uint64(i) {
				return false
			}
		}

		return true
	})
	require.NoError(t, err)

	checkStorage(t, txs, client)
}

func checkStorage(t *testing.T, txs []*framework.TestTxn, client *jsonrpc.EthClient) {
	t.Helper()

	for i, tx := range txs {
		bn, err := client.GetBlockByNumber(jsonrpc.BlockNumber(tx.Receipt().BlockNumber), true)
		require.NoError(t, err)
		assert.NotNil(t, bn)

		bh, err := client.GetBlockByHash(bn.Header.Hash, true)
		require.NoError(t, err)
		assert.NotNil(t, bh)

		if !reflect.DeepEqual(bn, bh) {
			t.Fatal("blocks dont match")
		}

		bt, err := client.GetTransactionByHash(types.Hash(tx.Receipt().TransactionHash))
		require.NoError(t, err)
		assert.NotNil(t, bt)
		assert.Equal(t, tx.Txn().Value(), bt.Value())
		assert.Equal(t, tx.Txn().Gas(), bt.Gas())
		assert.Equal(t, tx.Txn().Nonce(), bt.Nonce())
		assert.Equal(t, tx.Receipt().TransactionIndex, bt.TxnIndex)
		v, r, s := bt.RawSignatureValues()
		assert.NotNil(t, v)
		assert.NotNil(t, r)
		assert.NotNil(t, s)
		assert.Equal(t, tx.Txn().From().Bytes(), bt.From().Bytes())
		assert.Equal(t, tx.Txn().To().Bytes(), bt.To().Bytes())

		if i%2 == 0 {
			assert.Equal(t, types.DynamicFeeTxType, bt.Type())
			assert.Nil(t, bt.GasPrice()) // dynamic txs don't have gasPrice set
			assert.NotNil(t, bt.GasFeeCap())
			assert.NotNil(t, bt.GasTipCap())
			assert.NotNil(t, bt.ChainID())
		} else {
			// assert.Equal(t, ethgo.TransactionLegacy, bt.Type)
			assert.Equal(t, ethgo.Gwei(2).Uint64(), bt.GasPrice().Uint64())
		}

		receipt, err := client.GetTransactionReceipt(types.Hash(tx.Receipt().TransactionHash))
		require.NoError(t, err)
		assert.NotNil(t, r)
		assert.Equal(t, bt.TxnIndex, receipt.TransactionIndex)
		assert.Equal(t, bt.Hash(), types.Hash(receipt.TransactionHash))
		assert.Equal(t, bt.BlockHash, types.Hash(receipt.BlockHash))
		assert.Equal(t, bt.BlockNumber, receipt.BlockNumber)
		assert.NotEmpty(t, receipt.LogsBloom)
		assert.Equal(t, bt.To(), (*types.Address)(receipt.To))
	}
}
