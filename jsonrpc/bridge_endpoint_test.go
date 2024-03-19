package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestBridgeEndpoint(t *testing.T) {
	store := newMockStore()

	dispatcher := newTestDispatcher(t,
		hclog.NewNullLogger(),
		store,
		&dispatcherParams{
			chainID:                 0,
			priceLimit:              0,
			jsonRPCBatchLengthLimit: 20,
			blockRangeLimit:         1000,
		},
	)

	mockConnection, _ := newMockWsConnWithMsgCh()

	msg := []byte(`{
		"method": "bridge_generateExitProof",
		"params": ["0x0001"],
		"id": 1
	}`)

	data, err := dispatcher.HandleWs(msg, mockConnection)
	require.NoError(t, err)

	resp := new(SuccessResponse)
	require.NoError(t, json.Unmarshal(data, resp))
	require.Nil(t, resp.Error)
	require.NotNil(t, resp.Result)

	msg = []byte(`{
		"method": "bridge_getStateSyncProof",
		"params": ["0x1"],
		"id": 1
	}`)

	data, err = dispatcher.HandleWs(msg, mockConnection)
	require.NoError(t, err)

	resp = new(SuccessResponse)
	require.NoError(t, json.Unmarshal(data, resp))
	require.Nil(t, resp.Error)
	require.NotNil(t, resp.Result)
}

func TestBre(t *testing.T) {
	client, err := NewEthClient("http://localhost:10002")
	require.NoError(t, err)

	block, err := client.GetBlockByNumber(BlockNumber(10), true)
	require.NoError(t, err)
	require.NotNil(t, block)

	tx, err := client.GetTransactionByHash(block.Transactions[0].Hash())
	require.NoError(t, err)
	require.NotNil(t, tx)

	receipt, err := client.GetTransactionReceipt(tx.Hash())
	require.NoError(t, err)
	require.NotNil(t, receipt)

	chainID, err := client.ChainID()
	require.NoError(t, err)
	require.NotNil(t, chainID)

	addr := types.StringToAddress("0x85da99c8a7c2c95964c8efd687e95e632fc533d6")
	latestBN := LatestBlockNumber
	latest := BlockNumberOrHash{BlockNumber: &latestBN}

	nonce, err := client.GetNonce(addr, latest)
	require.NoError(t, err)
	require.NotNil(t, nonce)

	blockNum, err := client.BlockNumber()
	require.NoError(t, err)
	require.Greater(t, blockNum, uint64(0))

	gasPrice, err := client.GasPrice()
	require.NoError(t, err)
	require.GreaterOrEqual(t, gasPrice, uint64(0))

	balance, err := client.GetBalance(addr, latest)
	require.NoError(t, err)
	require.NotNil(t, balance)

	maxPriorityFeePerGas, err := client.MaxPriorityFeePerGas()
	require.NoError(t, err)
	require.NotNil(t, maxPriorityFeePerGas)
}
