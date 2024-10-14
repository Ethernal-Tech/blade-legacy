package jsonrpc

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"

	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer/structtracer"
	"github.com/0xPolygon/polygon-edge/types"
)

type debugEndpointMockStore struct {
	headerFn              func() *types.Header
	getHeaderByNumberFn   func(uint64) (*types.Header, bool)
	getReceiptsByHashFn   func(types.Hash) ([]*types.Receipt, error)
	readTxLookupFn        func(types.Hash) (uint64, bool)
	getPendingTxFn        func(types.Hash) (*types.Transaction, bool)
	hasFn                 func(types.Hash) bool
	getFn                 func(string) ([]byte, error)
	getIteratorDumpTreeFn func(*types.Block, *state.DumpInfo) (*state.IteratorDump, error)
	dumpTreeFn            func(*types.Block, *state.DumpInfo) (*state.Dump, error)
	getBlockByHashFn      func(types.Hash, bool) (*types.Block, bool)
	getBlockByNumberFn    func(uint64, bool) (*types.Block, bool)
	traceBlockFn          func(*types.Block, tracer.Tracer) ([]interface{}, error)
	intermediateRootsFn   func(*types.Block, tracer.Tracer) ([]types.Hash, error)
	storageRangeAtFn      func(*state.StorageRangeResult, *types.Block, *types.Address, []byte, int, int) error
	traceTxnFn            func(*types.Block, types.Hash, tracer.Tracer) (interface{}, error)
	traceCallFn           func(*types.Transaction, *types.Header, tracer.Tracer) (interface{}, error)
	getNonceFn            func(types.Address) uint64
	getAccountFn          func(types.Hash, types.Address) (*Account, error)
}

func (s *debugEndpointMockStore) Header() *types.Header {
	return s.headerFn()
}

func (s *debugEndpointMockStore) GetHeaderByNumber(num uint64) (*types.Header, bool) {
	return s.getHeaderByNumberFn(num)
}

func (s *debugEndpointMockStore) GetReceiptsByHash(hash types.Hash) ([]*types.Receipt, error) {
	return s.getReceiptsByHashFn(hash)
}

func (s *debugEndpointMockStore) ReadTxLookup(txnHash types.Hash) (uint64, bool) {
	return s.readTxLookupFn(txnHash)
}

func (s *debugEndpointMockStore) GetPendingTx(txHash types.Hash) (*types.Transaction, bool) {
	return s.getPendingTxFn(txHash)
}

func (s *debugEndpointMockStore) Has(hash types.Hash) bool {
	return s.hasFn(hash)
}

func (s *debugEndpointMockStore) Get(key string) ([]byte, error) {
	return s.getFn(key)
}

func (s *debugEndpointMockStore) GetIteratorDumpTree(block *types.Block, opts *state.DumpInfo) (*state.IteratorDump, error) {
	return s.getIteratorDumpTreeFn(block, opts)
}

func (s *debugEndpointMockStore) DumpTree(block *types.Block, opts *state.DumpInfo) (*state.Dump, error) {
	return s.dumpTreeFn(block, opts)
}

func (s *debugEndpointMockStore) GetBlockByHash(hash types.Hash, full bool) (*types.Block, bool) {
	return s.getBlockByHashFn(hash, full)
}

func (s *debugEndpointMockStore) GetBlockByNumber(num uint64, full bool) (*types.Block, bool) {
	return s.getBlockByNumberFn(num, full)
}

func (s *debugEndpointMockStore) TraceBlock(block *types.Block, tracer tracer.Tracer) ([]interface{}, error) {
	return s.traceBlockFn(block, tracer)
}

func (s *debugEndpointMockStore) IntermediateRoots(block *types.Block, tracer tracer.Tracer) ([]types.Hash, error) {
	return s.intermediateRootsFn(block, tracer)
}

func (s *debugEndpointMockStore) StorageRangeAt(storageRangeResult *state.StorageRangeResult, block *types.Block, addr *types.Address, keyStart []byte, txIndex, maxResult int) error {
	return s.storageRangeAtFn(storageRangeResult, block, addr, keyStart, txIndex, maxResult)
}

func (s *debugEndpointMockStore) TraceTxn(block *types.Block, targetTx types.Hash, tracer tracer.Tracer) (interface{}, error) {
	return s.traceTxnFn(block, targetTx, tracer)
}

func (s *debugEndpointMockStore) TraceCall(tx *types.Transaction, parent *types.Header, tracer tracer.Tracer) (interface{}, error) {
	return s.traceCallFn(tx, parent, tracer)
}

func (s *debugEndpointMockStore) GetNonce(acc types.Address) uint64 {
	return s.getNonceFn(acc)
}

func (s *debugEndpointMockStore) GetAccount(root types.Hash, addr types.Address) (*Account, error) {
	return s.getAccountFn(root, addr)
}

func TestDebugTraceConfigDecode(t *testing.T) {
	timeout15s := "15s"

	tests := []struct {
		input    string
		expected TraceConfig
	}{
		{
			// default
			input: `{}`,
			expected: TraceConfig{
				EnableMemory:     false,
				DisableStack:     false,
				DisableStorage:   false,
				EnableReturnData: false,
			},
		},
		{
			input: `{
				"enableMemory": true
			}`,
			expected: TraceConfig{
				EnableMemory:     true,
				DisableStack:     false,
				DisableStorage:   false,
				EnableReturnData: false,
			},
		},
		{
			input: `{
				"disableStack": true
			}`,
			expected: TraceConfig{
				EnableMemory:     false,
				DisableStack:     true,
				DisableStorage:   false,
				EnableReturnData: false,
			},
		},
		{
			input: `{
				"disableStorage": true
			}`,
			expected: TraceConfig{
				EnableMemory:     false,
				DisableStack:     false,
				DisableStorage:   true,
				EnableReturnData: false,
			},
		},
		{
			input: `{
				"enableReturnData": true
			}`,
			expected: TraceConfig{
				EnableMemory:     false,
				DisableStack:     false,
				DisableStorage:   false,
				EnableReturnData: true,
			},
		},
		{
			input: `{
				"timeout": "15s"
			}`,
			expected: TraceConfig{
				EnableMemory:     false,
				DisableStack:     false,
				DisableStorage:   false,
				EnableReturnData: false,
				Timeout:          &timeout15s,
			},
		},
		{
			input: `{
				"enableMemory": true,
				"disableStack": true,
				"disableStorage": true,
				"enableReturnData": true,
				"timeout": "15s"
			}`,
			expected: TraceConfig{
				EnableMemory:     true,
				DisableStack:     true,
				DisableStorage:   true,
				EnableReturnData: true,
				Timeout:          &timeout15s,
			},
		},
		{
			input: `{
				"disableStack": true,
				"disableStorage": true,
				"enableReturnData": true,
				"disableStructLogs": true
			}`,
			expected: TraceConfig{
				DisableStack:      true,
				DisableStorage:    true,
				EnableReturnData:  true,
				DisableStructLogs: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result := TraceConfig{}

			require.NoError(
				t,
				json.Unmarshal(
					[]byte(test.input),
					&result,
				),
			)

			require.Equal(
				t,
				test.expected,
				result,
			)
		})
	}
}

func TestTraceBlockByNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		blockNumber BlockNumber
		config      *TraceConfig
		store       *debugEndpointMockStore
		result      interface{}
		err         bool
	}{
		{
			name:        "should trace the latest block",
			blockNumber: LatestBlockNumber,
			config:      &TraceConfig{},
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestHeader
				},
				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testLatestHeader.Number, num)
					require.True(t, full)

					return testLatestBlock, true
				},
				traceBlockFn: func(block *types.Block, tracer tracer.Tracer) ([]interface{}, error) {
					require.Equal(t, testLatestBlock, block)

					return testTraceResults, nil
				},
			},
			result: testTraceResults,
			err:    false,
		},
		{
			name:        "should trace the block at the given height",
			blockNumber: 10,
			config:      &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testHeader10.Number, num)
					require.True(t, full)

					return testBlock10, true
				},
				traceBlockFn: func(block *types.Block, tracer tracer.Tracer) ([]interface{}, error) {
					require.Equal(t, testBlock10, block)

					return testTraceResults, nil
				},
			},
			result: testTraceResults,
			err:    false,
		},
		{
			name:        "should return errTraceGenesisBlock for genesis block",
			blockNumber: 0,
			config:      &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testGenesisHeader.Number, num)
					require.True(t, full)

					return testGenesisBlock, true
				},
			},
			result: nil,
			err:    true,
		},
		{
			name:        "should return errBlockNotFound",
			blockNumber: 11,
			config:      &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					require.Equal(t, uint64(11), num)
					require.True(t, full)

					return nil, false
				},
			},
			result: nil,
			err:    true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.TraceBlockByNumber(test.blockNumber, test.config)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPrintBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		number    uint64
		store     *debugEndpointMockStore
		returnErr string
		result    interface{}
		err       bool
	}{
		{
			name:   "block not found",
			number: testBlock10.Number(),
			store: &debugEndpointMockStore{
				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), num)
					require.True(t, full)

					return nil, false
				},
			},
			returnErr: "not found",
			result:    nil,
			err:       true,
		},
		{
			name:   "the block is written",
			number: testBlock10.Number(),
			store: &debugEndpointMockStore{
				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testHeader10.Number, num)
					require.True(t, full)

					return testBlock10, true
				},
			},
			returnErr: "",
			result:    spew.Sdump(testBlock10),
			err:       false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.PrintBlock(test.number)

			require.Equal(t, test.result, res)

			if test.err {
				require.ErrorContains(t, err, test.returnErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTraceBlockByHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		blockHash types.Hash
		config    *TraceConfig
		store     *debugEndpointMockStore
		result    interface{}
		err       bool
	}{
		{
			name:      "should trace the latest block",
			blockHash: testHeader10.Hash,
			config:    &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					require.Equal(t, testHeader10.Hash, hash)
					require.True(t, full)

					return testBlock10, true
				},
				traceBlockFn: func(block *types.Block, tracer tracer.Tracer) ([]interface{}, error) {
					require.Equal(t, testBlock10, block)

					return testTraceResults, nil
				},
			},
			result: testTraceResults,
			err:    false,
		},
		{
			name:      "should return errBlockNotFound",
			blockHash: testHash11,
			config:    &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					require.Equal(t, testHash11, hash)
					require.True(t, full)

					return nil, false
				},
			},
			result: nil,
			err:    true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.TraceBlockByHash(test.blockHash, test.config)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTraceBlock(t *testing.T) {
	t.Parallel()

	blockBytes := testLatestBlock.MarshalRLP()
	blockHex := hex.EncodeToHex(blockBytes)

	tests := []struct {
		name   string
		input  string
		config *TraceConfig
		store  *debugEndpointMockStore
		result interface{}
		err    bool
	}{
		{
			name:   "should trace the given block",
			input:  blockHex,
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				traceBlockFn: func(block *types.Block, tracer tracer.Tracer) ([]interface{}, error) {
					require.Equal(t, testLatestBlock, block)

					return testTraceResults, nil
				},
			},
			result: testTraceResults,
			err:    false,
		},
		{
			name:   "should return error in case of invalid block",
			input:  "hoge",
			config: &TraceConfig{},
			store:  &debugEndpointMockStore{},
			result: nil,
			err:    true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.TraceBlock(test.input, test.config)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTraceBlockFromFile(t *testing.T) {
	t.Parallel()

	blockBytes := testLatestBlock.MarshalRLP()

	tests := []struct {
		name    string
		input   []byte
		fileErr error
		config  *TraceConfig
		store   *debugEndpointMockStore
		result  interface{}
		err     bool
	}{
		{
			name:    "should follow the given block from the file",
			input:   blockBytes,
			fileErr: nil,
			config:  &TraceConfig{},
			store: &debugEndpointMockStore{
				traceBlockFn: func(block *types.Block, tracer tracer.Tracer) ([]interface{}, error) {
					require.Equal(t, testLatestBlock, block)

					return testTraceResults, nil
				},
			},
			result: testTraceResults,
			err:    false,
		},
		{
			name:    "should return error in case of invalid block from the file",
			input:   nil,
			fileErr: fmt.Errorf("file is incorrect"),
			config:  &TraceConfig{},
			store:   &debugEndpointMockStore{},
			result:  nil,
			err:     true,
		},
		{
			name:    "should return error in case of invalid block from the UnmarshalRLP method",
			input:   []byte{},
			fileErr: nil,
			config:  &TraceConfig{},
			store:   &debugEndpointMockStore{},
			result:  nil,
			err:     true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)

			endpoint.ReadFileFunc = func(filename string) ([]byte, error) {
				return test.input, test.fileErr
			}

			res, err := endpoint.TraceBlockFromFile("testfile.txt", test.config)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTraceTransaction(t *testing.T) {
	t.Parallel()

	blockWithTx := &types.Block{
		Header: testBlock10.Header,
		Transactions: []*types.Transaction{
			testTx1,
		},
	}

	tests := []struct {
		name   string
		txHash types.Hash
		config *TraceConfig
		store  *debugEndpointMockStore
		result interface{}
		err    bool
	}{
		{
			name:   "should trace the given transaction",
			txHash: testTxHash1,
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return testBlock10.Number(), true
				},
				getBlockByNumberFn: func(number uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), number)
					require.True(t, full)

					return blockWithTx, true
				},
				traceTxnFn: func(block *types.Block, txHash types.Hash, tracer tracer.Tracer) (interface{}, error) {
					require.Equal(t, blockWithTx, block)
					require.Equal(t, testTxHash1, txHash)

					return testTraceResult, nil
				},
			},
			result: testTraceResult,
			err:    false,
		},
		{
			name:   "should return error if ReadTxLookup returns null",
			txHash: testTxHash1,
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return 0, false
				},
			},
			result: nil,
			err:    true,
		},
		{
			name:   "should return error if block not found",
			txHash: testTxHash1,
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return testBlock10.Number(), true
				},
				getBlockByNumberFn: func(number uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), number)
					require.True(t, full)

					return nil, false
				},
			},
			result: nil,
			err:    true,
		},
		{
			name:   "should return error if the tx is not including the block",
			txHash: testTxHash1,
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return testBlock10.Number(), true
				},
				getBlockByNumberFn: func(number uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), number)
					require.True(t, full)

					return testBlock10, true
				},
			},
			result: nil,
			err:    true,
		},
		{
			name:   "should return error if the block is genesis",
			txHash: testTxHash1,
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return testBlock10.Number(), true
				},
				getBlockByNumberFn: func(number uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), number)
					require.True(t, full)

					return &types.Block{
						Header: testGenesisHeader,
						Transactions: []*types.Transaction{
							testTx1,
						},
					}, true
				},
			},
			result: nil,
			err:    true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.TraceTransaction(test.txHash, test.config)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTraceCall(t *testing.T) {
	t.Parallel()

	var (
		from      = types.StringToAddress("1")
		to        = types.StringToAddress("2")
		gas       = argUint64(10000)
		gasPrice  = argBytes(new(big.Int).SetUint64(10).Bytes())
		gasTipCap = argBytes(new(big.Int).SetUint64(10).Bytes())
		gasFeeCap = argBytes(new(big.Int).SetUint64(10).Bytes())
		value     = argBytes(new(big.Int).SetUint64(1000).Bytes())
		data      = argBytes([]byte("data"))
		input     = argBytes([]byte("input"))
		nonce     = argUint64(1)

		blockNumber = BlockNumber(testBlock10.Number())

		txArg = &txnArgs{
			From:      &from,
			To:        &to,
			Gas:       &gas,
			GasPrice:  &gasPrice,
			GasTipCap: &gasTipCap,
			GasFeeCap: &gasFeeCap,
			Value:     &value,
			Data:      &data,
			Input:     &input,
			Nonce:     &nonce,
			Type:      toArgUint64Ptr(uint64(types.DynamicFeeTxType)),
		}
		decodedTx = types.NewTx(types.NewDynamicFeeTx(
			types.WithGasTipCap(new(big.Int).SetBytes([]byte(gasTipCap))),
			types.WithGasFeeCap(new(big.Int).SetBytes([]byte(gasFeeCap))),
			types.WithNonce(uint64(nonce)),
			types.WithGas(uint64(gas)),
			types.WithTo(&to),
			types.WithValue(new(big.Int).SetBytes([]byte(value))),
			types.WithInput(data),
			types.WithFrom(from),
		))
	)

	decodedTx.ComputeHash()

	tests := []struct {
		name   string
		arg    *txnArgs
		filter BlockNumberOrHash
		config *TraceConfig
		store  *debugEndpointMockStore
		result interface{}
		err    bool
	}{
		{
			name: "should trace the given transaction",
			arg:  txArg,
			filter: BlockNumberOrHash{
				BlockNumber: &blockNumber,
			},
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				getHeaderByNumberFn: func(num uint64) (*types.Header, bool) {
					require.Equal(t, testBlock10.Number(), num)

					return testHeader10, true
				},
				traceCallFn: func(tx *types.Transaction, header *types.Header, tracer tracer.Tracer) (interface{}, error) {
					require.Equal(t, decodedTx, tx)
					require.Equal(t, testHeader10, header)

					return testTraceResult, nil
				},
				headerFn: func() *types.Header {
					return testLatestHeader
				},
				getAccountFn: func(h types.Hash, a types.Address) (*Account, error) {
					return &Account{Nonce: 1}, nil
				},
			},
			result: testTraceResult,
			err:    false,
		},
		{
			name: "should return error if block not found",
			arg:  txArg,
			filter: BlockNumberOrHash{
				BlockHash: &testHeader10.Hash,
			},
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					require.Equal(t, testHeader10.Hash, hash)
					require.False(t, full)

					return nil, false
				},
				headerFn: func() *types.Header {
					return testLatestHeader
				},
				getAccountFn: func(h types.Hash, a types.Address) (*Account, error) {
					return &Account{Nonce: 1}, nil
				},
			},
			result: nil,
			err:    true,
		},
		{
			name: "should return error if decoding transaction fails",
			arg: &txnArgs{
				From:     &from,
				Gas:      &gas,
				GasPrice: &gasPrice,
				Value:    &value,
				Nonce:    &nonce,
			},
			filter: BlockNumberOrHash{},
			config: &TraceConfig{},
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestHeader
				},
				getAccountFn: func(h types.Hash, a types.Address) (*Account, error) {
					return &Account{Nonce: 1}, nil
				},
			},
			result: nil,
			err:    true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.TraceCall(test.arg, test.filter, test.config)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetRawBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter BlockNumberOrHash
		store  *debugEndpointMockStore
		result interface{}
		err    bool
	}{
		{
			name:   "HeaderNotFound",
			filter: BlockNumberOrHash{},
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return nil, false
				},
			},
			result: nil,
			err:    true,
		},
		{
			name:   "BlockNotFound",
			filter: LatestBlockNumberOrHash,

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return nil, false
				},
			},
			result: nil,
			err:    true,
		},
		{
			name:   "Success",
			filter: LatestBlockNumberOrHash,
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},
			},
			result: testLatestBlock.MarshalRLP(),
			err:    false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.GetRawBlock(test.filter)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetRawHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter BlockNumberOrHash
		store  *debugEndpointMockStore
		result interface{}
		err    bool
	}{
		{
			name:   "HeaderNotFound",
			filter: BlockNumberOrHash{BlockHash: &hash1},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return nil, false
				},
			},
			result: nil,
			err:    true,
		},
		{
			name:   "Success",
			filter: LatestBlockNumberOrHash,
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},
			},
			result: testLatestBlock.Header.MarshalRLP(),
			err:    false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.GetRawHeader(test.filter)

			require.Equal(t, test.result, res)

			if test.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetRawTransaction(t *testing.T) {
	t.Parallel()

	blockWithTx := &types.Block{
		Header: testBlock10.Header,
		Transactions: []*types.Transaction{
			testTx1,
		},
	}

	tests := []struct {
		name   string
		txHash types.Hash
		store  *debugEndpointMockStore
		result interface{}
	}{
		{
			name:   "successful because ReadTxLookup is filled",
			txHash: testTxHash1,
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return testBlock10.Number(), true
				},
				getBlockByNumberFn: func(number uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), number)
					require.True(t, full)

					return blockWithTx, true
				},
			},
			result: blockWithTx.Transactions[0].MarshalRLP(),
		},
		{
			name:   "should return error if ReadTxLookup and GetPendingTx returns null",
			txHash: testTxHash1,
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return 0, false
				},
				getPendingTxFn: func(hash types.Hash) (*types.Transaction, bool) {
					require.Equal(t, testTxHash1, hash)

					return nil, false
				},
			},
			result: nil,
		},
		{
			name:   "should return error if block not found",
			txHash: testTxHash1,
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return testBlock10.Number(), true
				},
				getBlockByNumberFn: func(number uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), number)
					require.True(t, full)

					return nil, false
				},
				getPendingTxFn: func(hash types.Hash) (*types.Transaction, bool) {
					require.Equal(t, testTxHash1, hash)

					return nil, false
				},
			},
			result: nil,
		},
		{
			name:   "should return error if the tx is not including the block",
			txHash: testTxHash1,
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return testBlock10.Number(), true
				},
				getBlockByNumberFn: func(number uint64, full bool) (*types.Block, bool) {
					require.Equal(t, testBlock10.Number(), number)
					require.True(t, full)

					return testBlock10, true
				},
				getPendingTxFn: func(hash types.Hash) (*types.Transaction, bool) {
					require.Equal(t, testTxHash1, hash)

					return nil, false
				},
			},
			result: nil,
		},
		{
			name:   "should succeed if GetPendingTx succeeds",
			txHash: testTxHash1,
			store: &debugEndpointMockStore{
				readTxLookupFn: func(hash types.Hash) (uint64, bool) {
					require.Equal(t, testTxHash1, hash)

					return 0, false
				},
				getPendingTxFn: func(hash types.Hash) (*types.Transaction, bool) {
					require.Equal(t, testTxHash1, hash)

					return blockWithTx.Transactions[0], true
				},
			},
			result: blockWithTx.Transactions[0].MarshalRLP(),
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, _ := endpoint.GetRawTransaction(test.txHash)

			require.Equal(t, test.result, res)
		})
	}
}

func TestGetRawReceipts(t *testing.T) {
	t.Parallel()

	const (
		cumulativeGasUsed = 28000
		gasUsed           = 26000
	)

	rec := createTestReceipt(nil, cumulativeGasUsed, gasUsed, testTx1.Hash())
	receipts := []*types.Receipt{rec}

	tests := []struct {
		name      string
		filter    BlockNumberOrHash
		store     *debugEndpointMockStore
		result    interface{}
		returnErr string
		err       bool
	}{
		{
			name:   "HeaderNotFound",
			filter: EarliestBlockNumberOrHash,
			store: &debugEndpointMockStore{
				getHeaderByNumberFn: func(num uint64) (*types.Header, bool) {
					require.Equal(t, uint64(0), num)

					return nil, false
				},
			},

			returnErr: "failed to get the header of block",
			result:    nil,
			err:       true,
		},
		{
			name:   "ReceiptsNotFound",
			filter: LatestBlockNumberOrHash,

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},
				getReceiptsByHashFn: func(hash types.Hash) ([]*types.Receipt, error) {
					return nil, fmt.Errorf("receipts not found")
				},
			},

			returnErr: "receipts not found",
			result:    nil,
			err:       true,
		},
		{
			name:   "Success",
			filter: LatestBlockNumberOrHash,
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},

				getReceiptsByHashFn: func(hash types.Hash) ([]*types.Receipt, error) {
					return receipts, nil
				},
			},

			returnErr: "",
			result:    [][]byte{rec.MarshalRLP()},
			err:       false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.GetRawReceipts(test.filter)

			require.Equal(t, test.result, res)

			if test.err {
				require.ErrorContains(t, err, test.returnErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAccountRange(t *testing.T) {
	t.Parallel()

	dumpIter := &state.IteratorDump{
		Next: addr2[:],
	}

	dumpEmpty := state.IteratorDump{}

	tests := []struct {
		name      string
		filter    BlockNumberOrHash
		store     *debugEndpointMockStore
		result    interface{}
		returnErr string
		err       bool
	}{
		{
			name:   "HeaderNotFound",
			filter: EarliestBlockNumberOrHash,
			store: &debugEndpointMockStore{
				getHeaderByNumberFn: func(num uint64) (*types.Header, bool) {
					require.Equal(t, uint64(0), num)

					return nil, false
				},
			},

			returnErr: "failed to get header",
			result:    dumpEmpty,
			err:       true,
		},
		{
			name:   "BlockNotFound",
			filter: LatestBlockNumberOrHash,

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return nil, false
				},
			},

			returnErr: "block not found for hash",
			result:    dumpEmpty,
			err:       true,
		},
		{
			name:   "IteratorDumpTreeError",
			filter: LatestBlockNumberOrHash,
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},

				getIteratorDumpTreeFn: func(block *types.Block, dump *state.DumpInfo) (*state.IteratorDump, error) {
					require.Equal(t, testLatestBlock, block)

					return nil, fmt.Errorf("IteratorDump not valid")
				},
			},

			returnErr: "failed to get iterator dump tree",
			result:    dumpEmpty,
			err:       true,
		},

		{
			name:   "IteratorDumpTreeValid",
			filter: LatestBlockNumberOrHash,
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},

				getIteratorDumpTreeFn: func(block *types.Block, dump *state.DumpInfo) (*state.IteratorDump, error) {
					require.Equal(t, testLatestBlock, block)

					return dumpIter, nil
				},
			},

			returnErr: "",
			result:    dumpIter,
			err:       false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)

			res, err := endpoint.AccountRange(test.filter, []byte{}, 1, false, false, false)

			require.Equal(t, test.result, res)

			if test.err {
				require.ErrorContains(t, err, test.returnErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDumpBlock(t *testing.T) {
	t.Parallel()

	dump := &state.Dump{}

	tests := []struct {
		name        string
		blockNumber BlockNumber
		store       *debugEndpointMockStore
		result      interface{}
		returnErr   string
		err         bool
	}{
		{
			name:        "GetNumericBlockNumberNotValid",
			blockNumber: BlockNumber(-5),
			store:       &debugEndpointMockStore{},
			returnErr:   "failed to get block number",
			result:      nil,
			err:         true,
		},
		{
			name:        "BlockNotFound",
			blockNumber: *LatestBlockNumberOrHash.BlockNumber,

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					return nil, false
				},
			},

			returnErr: "block not found for number ",
			result:    nil,
			err:       true,
		},
		{
			name:        "DumpTreeError",
			blockNumber: *LatestBlockNumberOrHash.BlockNumber,
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},

				dumpTreeFn: func(block *types.Block, dump *state.DumpInfo) (*state.Dump, error) {
					require.Equal(t, testLatestBlock, block)

					return nil, fmt.Errorf("dump not valid")
				},
			},

			returnErr: "failed to dump tree",
			result:    nil,
			err:       true,
		},

		{
			name:        "DumpTreeValid",
			blockNumber: *LatestBlockNumberOrHash.BlockNumber,
			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getBlockByNumberFn: func(num uint64, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},

				dumpTreeFn: func(block *types.Block, dumpInfo *state.DumpInfo) (*state.Dump, error) {
					require.Equal(t, testLatestBlock, block)

					return dump, nil
				},
			},

			returnErr: "",
			result:    dump,
			err:       false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)

			res, err := endpoint.DumpBlock(test.blockNumber)

			require.Equal(t, test.result, res)

			if test.err {
				require.ErrorContains(t, err, test.returnErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetAccessibleState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		start, end BlockNumber
		store      *debugEndpointMockStore
		result     interface{}
		returnErr  string
		err        bool
	}{
		{
			name:      "GetNumericBlockNumberNotValid",
			start:     BlockNumber(-5),
			end:       BlockNumber(-5),
			store:     &debugEndpointMockStore{},
			returnErr: "failed to get block number",
			result:    0,
			err:       true,
		},
		{
			name:  "BlockNotDifferent",
			start: *LatestBlockNumberOrHash.BlockNumber,
			end:   *LatestBlockNumberOrHash.BlockNumber,

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getHeaderByNumberFn: func(num uint64) (*types.Header, bool) {
					return nil, false
				},
			},

			returnErr: "no accessible state found in the block",
			result:    0,
			err:       true,
		},
		{
			name:  "HeaderNotFound",
			start: *LatestBlockNumberOrHash.BlockNumber,
			end:   BlockNumber(testHeader10.Number),

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getHeaderByNumberFn: func(num uint64) (*types.Header, bool) {
					return nil, false
				},
			},

			returnErr: "missing header for block number",
			result:    0,
			err:       true,
		},
		{
			name:  "resultNotFound",
			start: *LatestBlockNumberOrHash.BlockNumber,
			end:   BlockNumber(testHeader10.Number),

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getHeaderByNumberFn: func(num uint64) (*types.Header, bool) {
					return testLatestBlock.Header, true
				},

				hasFn: func(hash types.Hash) bool {
					return false
				},
			},

			returnErr: "no accessible state found between the block numbers 100 and 10",
			result:    0,
			err:       true,
		},

		{
			name:  "resultsValid",
			start: *LatestBlockNumberOrHash.BlockNumber,
			end:   BlockNumber(testHeader10.Number),

			store: &debugEndpointMockStore{
				headerFn: func() *types.Header {
					return testLatestBlock.Header
				},

				getHeaderByNumberFn: func(num uint64) (*types.Header, bool) {
					return testLatestBlock.Header, true
				},

				hasFn: func(hash types.Hash) bool {
					return true
				},
			},

			returnErr: "",
			result:    testLatestBlock.Header.Number,
			err:       false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)

			res, err := endpoint.GetAccessibleState(test.start, test.end)

			require.Equal(t, test.result, res)

			if test.err {
				require.ErrorContains(t, err, test.returnErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIntermediateRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		blockHash types.Hash
		config    *TraceConfig
		store     *debugEndpointMockStore
		result    interface{}
		returnErr string
		err       bool
	}{
		{
			name:      "BlockByHashNotFound",
			blockHash: testLatestBlock.Hash(),
			config:    &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					require.Equal(t, testLatestBlock.Hash(), hash)
					require.True(t, full)

					return nil, false
				},
			},

			returnErr: "not found",
			result:    nil,
			err:       true,
		},

		{
			name:      "intermediateRootsNotValid",
			blockHash: testLatestBlock.Hash(),
			config:    &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},
				intermediateRootsFn: func(block *types.Block, tracer tracer.Tracer) ([]types.Hash, error) {
					require.Equal(t, block.Hash(), testLatestBlock.Hash())

					return []types.Hash{}, fmt.Errorf("roots not valid")
				},
			},

			returnErr: "roots not valid",
			result:    []types.Hash{},
			err:       true,
		},

		{
			name:      "resultsValid",
			blockHash: testLatestBlock.Hash(),
			config:    &TraceConfig{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},
				intermediateRootsFn: func(block *types.Block, tracer tracer.Tracer) ([]types.Hash, error) {
					require.Equal(t, block.Hash(), testLatestBlock.Hash())

					return []types.Hash{block.Hash()}, nil
				},
			},

			returnErr: "",
			result:    []types.Hash{testLatestBlock.Hash()},
			err:       false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.IntermediateRoots(test.blockHash, test.config)
			require.Equal(t, test.result, res)

			if test.err {
				require.ErrorContains(t, err, test.returnErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStorageRangeAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		blockHash          types.Hash
		storageRangeResult state.StorageRangeResult
		store              *debugEndpointMockStore
		result             interface{}
		returnErr          string
		err                bool
	}{
		{
			name:      "BlockByHashNotFound",
			blockHash: testLatestBlock.Hash(),
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					require.Equal(t, testLatestBlock.Hash(), hash)
					require.True(t, full)

					return nil, false
				},
			},

			returnErr: "not found",
			result:    nil,
			err:       true,
		},

		{
			name:               "storageRangeAtNotValid",
			blockHash:          testLatestBlock.Hash(),
			storageRangeResult: state.StorageRangeResult{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},
				storageRangeAtFn: func(srr *state.StorageRangeResult, block *types.Block, address *types.Address, start []byte, txIndex, maxCount int) error {
					require.Equal(t, block.Hash(), testLatestBlock.Hash())

					return fmt.Errorf("storageRangeAt not valid")
				},
			},

			returnErr: "storageRangeAt not valid",
			result:    state.StorageRangeResult{},
			err:       true,
		},

		{
			name:               "resultsValid",
			blockHash:          testLatestBlock.Hash(),
			storageRangeResult: state.StorageRangeResult{},
			store: &debugEndpointMockStore{
				getBlockByHashFn: func(hash types.Hash, full bool) (*types.Block, bool) {
					return testLatestBlock, true
				},
				storageRangeAtFn: func(srr *state.StorageRangeResult, block *types.Block, address *types.Address, start []byte, txIndex, maxCount int) error {
					require.Equal(t, block.Hash(), testLatestBlock.Hash())

					return nil
				},
			},

			returnErr: "",
			result:    state.StorageRangeResult{},
			err:       false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			endpoint := NewDebug(test.store, 100000)
			res, err := endpoint.StorageRangeAt(test.blockHash, 0, addr0, []byte{}, 10)
			require.Equal(t, test.result, res)

			if test.err {
				require.ErrorContains(t, err, test.returnErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_newTracer(t *testing.T) {
	t.Parallel()

	t.Run("should create tracer", func(t *testing.T) {
		t.Parallel()

		tracer, cancel, err := newTracer(&TraceConfig{
			EnableMemory:     true,
			EnableReturnData: true,
			DisableStack:     false,
			DisableStorage:   false,
		})

		t.Cleanup(func() {
			cancel()
		})

		require.NotNil(t, tracer)
		require.NoError(t, err)
	})

	t.Run("should return error if arg is nil", func(t *testing.T) {
		t.Parallel()

		tracer, cancel, err := newTracer(nil)

		require.Nil(t, tracer)
		require.Nil(t, cancel)
		require.ErrorIs(t, ErrNoConfig, err)
	})

	t.Run("GetResult should return errExecutionTimeout if timeout happens", func(t *testing.T) {
		t.Parallel()

		timeout := "0s"
		tracer, cancel, err := newTracer(&TraceConfig{
			EnableMemory:     true,
			EnableReturnData: true,
			DisableStack:     false,
			DisableStorage:   false,
			Timeout:          &timeout,
		})

		t.Cleanup(func() {
			cancel()
		})

		require.NoError(t, err)

		// wait until timeout
		time.Sleep(100 * time.Millisecond)

		res, err := tracer.GetResult()
		require.Nil(t, res)
		require.Equal(t, ErrExecutionTimeout, err)
	})

	t.Run("GetResult should not return if cancel is called beforre timeout", func(t *testing.T) {
		t.Parallel()

		timeout := "5s"
		tracer, cancel, err := newTracer(&TraceConfig{
			EnableMemory:     true,
			EnableReturnData: true,
			DisableStack:     false,
			DisableStorage:   false,
			Timeout:          &timeout,
		})

		require.NoError(t, err)

		cancel()

		res, err := tracer.GetResult()

		require.NotNil(t, res)
		require.NoError(t, err)
	})

	t.Run("should disable everything if struct logs are disabled", func(t *testing.T) {
		t.Parallel()

		tracer, cancel, err := newTracer(&TraceConfig{
			EnableMemory:      true,
			EnableReturnData:  true,
			DisableStack:      false,
			DisableStorage:    false,
			DisableStructLogs: true,
		})

		t.Cleanup(func() {
			cancel()
		})

		require.NoError(t, err)

		st, ok := tracer.(*structtracer.StructTracer)
		require.True(t, ok)

		require.Equal(t, structtracer.Config{
			EnableMemory:     false,
			EnableStack:      false,
			EnableStorage:    false,
			EnableReturnData: true,
			EnableStructLogs: false,
		}, st.Config)
	})
}
