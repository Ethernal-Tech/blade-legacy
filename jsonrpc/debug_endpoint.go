package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer/calltracer"
	js "github.com/0xPolygon/polygon-edge/state/runtime/tracer/jstracer"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer/structtracer"
	"github.com/0xPolygon/polygon-edge/types"
)

const callTracerName = "callTracer"

var (
	defaultTraceTimeout = 5 * time.Minute

	// ErrExecutionTimeout indicates the execution was terminated due to timeout
	ErrExecutionTimeout = errors.New("execution timeout")
	// ErrTraceGenesisBlock is an error returned when tracing genesis block which can't be traced
	ErrTraceGenesisBlock = errors.New("genesis is not traceable")
	// ErrNoConfig is an error returns when config is empty
	ErrNoConfig = errors.New("missing config object")

	jsKeywords = []string{"function", "var", "let", "const", "if", "else", "for", "while", "switch", "return"}
)

type debugBlockchainStore interface {
	// Header returns the current header of the chain (genesis if empty)
	Header() *types.Header

	// GetHeaderByNumber gets a header using the provided number
	GetHeaderByNumber(uint64) (*types.Header, bool)

	// ReadTxLookup returns a block hash in which a given txn was mined
	ReadTxLookup(txnHash types.Hash) (uint64, bool)

	// GetBlockByHash gets a block using the provided hash
	GetBlockByHash(hash types.Hash, full bool) (*types.Block, bool)

	// GetBlockByNumber gets a block using the provided height
	GetBlockByNumber(num uint64, full bool) (*types.Block, bool)

	// TraceBlock traces all transactions in the given block
	TraceBlock(*types.Block, runtime.Tracer) ([]interface{}, error)

	// TraceTxn traces a transaction in the block, associated with the given hash
	TraceTxn(*types.Block, types.Hash, runtime.Tracer) (interface{}, error)

	// TraceCall traces a single call at the point when the given header is mined
	TraceCall(*types.Transaction, *types.Header, types.StateOverride, runtime.Tracer) (interface{}, error)
}

type debugTxPoolStore interface {
	GetNonce(types.Address) uint64
}

type debugStateStore interface {
	GetAccount(root types.Hash, addr types.Address) (*Account, error)
}

type debugStore interface {
	debugBlockchainStore
	debugTxPoolStore
	debugStateStore
}

// Debug is the debug jsonrpc endpoint
type Debug struct {
	store      debugStore
	throttling *Throttling
}

func NewDebug(store debugStore, requestsPerSecond uint64) *Debug {
	return &Debug{
		store:      store,
		throttling: NewThrottling(requestsPerSecond, time.Second),
	}
}

// BlockOverrides is a set of header fields to override.
type BlockOverrides struct {
	Number     *argBig
	Difficulty *argBig
	Time       *argUint64
	GasLimit   *argUint64
	Coinbase   *types.Address
	BaseFee    *argBig
}

// Apply overrides the given header fields into the given block context.
func (bo *BlockOverrides) Apply(header *types.Header) {
	if bo == nil {
		return
	}

	if bo.Number != nil {
		header.Number = bo.Number.ToInt().Uint64()
	}

	if bo.Difficulty != nil {
		header.Difficulty = bo.Difficulty.ToInt().Uint64()
	}

	if bo.Time != nil {
		header.Timestamp = uint64(*bo.Time)
	}

	if bo.GasLimit != nil {
		header.GasLimit = uint64(*bo.GasLimit)
	}

	if bo.Coinbase != nil {
		header.Miner = bo.Coinbase.Bytes()
	}

	if bo.BaseFee != nil {
		header.BaseFee = bo.BaseFee.ToInt().Uint64()
	}
}

type TraceConfig struct {
	EnableMemory      bool            `json:"enableMemory"`
	DisableStack      bool            `json:"disableStack"`
	DisableStorage    bool            `json:"disableStorage"`
	EnableReturnData  bool            `json:"enableReturnData"`
	DisableStructLogs bool            `json:"disableStructLogs"`
	Timeout           *string         `json:"timeout"`
	Tracer            string          `json:"tracer"`
	TracerConfig      json.RawMessage `json:"tracerConfig"` // Config specific to given tracer
}

// TraceCallConfig is the config for traceCall API. It holds one more
// field to override the state for tracing.
type TraceCallConfig struct {
	TraceConfig
	StateOverrides *StateOverride  `json:"stateOverrides"`
	BlockOverrides *BlockOverrides `json:"blockOverrides"`
}

func (d *Debug) TraceBlockByNumber(
	blockNumber BlockNumber,
	config *TraceConfig,
) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			num, err := GetNumericBlockNumber(blockNumber, d.store)
			if err != nil {
				return nil, err
			}

			block, ok := d.store.GetBlockByNumber(num, true)
			if !ok {
				return nil, fmt.Errorf("block %d not found", num)
			}

			return d.traceBlock(block, config)
		},
	)
}

func (d *Debug) TraceBlockByHash(
	blockHash types.Hash,
	config *TraceConfig,
) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			block, ok := d.store.GetBlockByHash(blockHash, true)
			if !ok {
				return nil, fmt.Errorf("block %s not found", blockHash)
			}

			return d.traceBlock(block, config)
		},
	)
}

func (d *Debug) TraceBlock(
	input string,
	config *TraceConfig,
) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			blockByte, decodeErr := hex.DecodeHex(input)
			if decodeErr != nil {
				return nil, fmt.Errorf("unable to decode block, %w", decodeErr)
			}

			block := &types.Block{}
			if err := block.UnmarshalRLP(blockByte); err != nil {
				return nil, err
			}

			return d.traceBlock(block, config)
		},
	)
}

func (d *Debug) TraceTransaction(
	txHash types.Hash,
	config *TraceConfig,
) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			tx, block, txIndex := GetTxAndBlockByTxHash(txHash, d.store)
			if tx == nil {
				return nil, fmt.Errorf("tx %s not found", txHash.String())
			}

			if block.Number() == 0 {
				return nil, ErrTraceGenesisBlock
			}

			txCtx := &runtime.TracerContext{
				BlockHash:   block.Hash(),
				BlockNumber: new(big.Int).SetUint64(block.Number()),
				TxIndex:     txIndex,
				TxHash:      tx.Hash(),
			}

			tracer, cancel, err := newTracer(config, txCtx)
			if err != nil {
				return nil, err
			}

			defer cancel()

			return d.store.TraceTxn(block, tx.Hash(), tracer)
		},
	)
}

func (d *Debug) TraceCall(
	arg *txnArgs,
	filter BlockNumberOrHash,
	config *TraceCallConfig,
) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			header, err := GetHeaderFromBlockNumberOrHash(filter, d.store)
			if err != nil {
				return nil, ErrHeaderNotFound
			}

			header = header.Copy()
			header.BaseFee = 0
			config.BlockOverrides.Apply(header)

			tx, err := DecodeTxn(arg, d.store, true)
			if err != nil {
				return nil, err
			}

			// If the caller didn't supply the gas limit in the message, then we set it to maximum possible => block gas limit
			if tx.Gas() == 0 {
				tx.SetGas(header.GasLimit)
			}

			tracer, cancel, err := newTracer(&config.TraceConfig, new(runtime.TracerContext))
			if err != nil {
				return nil, err
			}

			defer cancel()

			var override types.StateOverride
			if config.StateOverrides != nil {
				override = config.StateOverrides.ToType()
			}

			return d.store.TraceCall(tx, header, override, tracer)
		},
	)
}

func (d *Debug) traceBlock(
	block *types.Block,
	config *TraceConfig,
) (interface{}, error) {
	if block.Number() == 0 {
		return nil, ErrTraceGenesisBlock
	}

	tracer, cancel, err := newTracer(config, nil)
	if err != nil {
		return nil, err
	}

	defer cancel()

	return d.store.TraceBlock(block, tracer)
}

// newTracer creates new tracer by config
func newTracer(config *TraceConfig, tracerCtx *runtime.TracerContext) (
	runtime.Tracer,
	context.CancelFunc,
	error,
) {
	var (
		timeout = defaultTraceTimeout
		tracer  runtime.Tracer
		err     error
	)

	if config == nil {
		return nil, nil, ErrNoConfig
	}

	if config.Timeout != nil {
		if timeout, err = time.ParseDuration(*config.Timeout); err != nil {
			return nil, nil, err
		}
	}

	if config.Tracer == callTracerName {
		tracer = &calltracer.CallTracer{}
	} else if isJavaScriptCode(config.Tracer) {
		tracer, err = js.NewJsTracer(config.Tracer, tracerCtx, config.TracerConfig)
	} else {
		tracer = structtracer.NewStructTracer(structtracer.Config{
			EnableMemory:     config.EnableMemory && !config.DisableStructLogs,
			EnableStack:      !config.DisableStack && !config.DisableStructLogs,
			EnableStorage:    !config.DisableStorage && !config.DisableStructLogs,
			EnableReturnData: config.EnableReturnData,
			EnableStructLogs: !config.DisableStructLogs,
		})
	}

	timeoutCtx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		<-timeoutCtx.Done()

		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			tracer.Cancel(ErrExecutionTimeout)
		}
	}()

	// cancellation of context is done by caller
	return tracer, cancel, nil
}

// isJavaScriptCode checks if the given string contains any JavaScript keywords.
// It iterates over a list of JavaScript keywords and checks if any of them are present in the string.
// If a keyword is found, it returns true; otherwise, it returns false.
func isJavaScriptCode(s string) bool {
	for _, keyword := range jsKeywords {
		if strings.Contains(s, keyword) {
			return true
		}
	}

	return false
}
