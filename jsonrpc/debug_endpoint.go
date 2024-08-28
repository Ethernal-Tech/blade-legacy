package jsonrpc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer/calltracer"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer/structtracer"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	callTracerName = "callTracer"
	blockString    = "block"
	mutexString    = "mutex"
	heapString     = "heap"
)

var (
	defaultTraceTimeout = 5 * time.Second

	// ErrExecutionTimeout indicates the execution was terminated due to timeout
	ErrExecutionTimeout = errors.New("execution timeout")
	// ErrTraceGenesisBlock is an error returned when tracing genesis block which can't be traced
	ErrTraceGenesisBlock = errors.New("genesis is not traceable")
	// ErrNoConfig is an error returns when config is empty
	ErrNoConfig = errors.New("missing config object")
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
	TraceBlock(*types.Block, tracer.Tracer) ([]interface{}, error)

	// TraceTxn traces a transaction in the block, associated with the given hash
	TraceTxn(*types.Block, types.Hash, tracer.Tracer) (interface{}, error)

	// TraceCall traces a single call at the point when the given header is mined
	TraceCall(*types.Transaction, *types.Header, tracer.Tracer) (interface{}, error)
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
	store        debugStore
	throttling   *Throttling
	handler      *DebugHandler
	ReadFileFunc func(filename string) ([]byte, error)
}

func NewDebug(store debugStore, requestsPerSecond uint64) *Debug {
	return &Debug{
		store:        store,
		throttling:   NewThrottling(requestsPerSecond, time.Second),
		handler:      new(DebugHandler),
		ReadFileFunc: os.ReadFile,
	}
}

// CPUProfile turns on CPU profiling for nsec seconds and writes
// profile data to file.
func (d *Debug) CPUProfile(file string, nsec int64) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			if err := d.handler.StartCPUProfile(file); err != nil {
				return nil, err
			}

			time.Sleep(time.Duration(nsec) * time.Second)

			if err := d.handler.StopCPUProfile(); err != nil {
				return nil, err
			}

			return nil, nil
		},
	)
}

// FreeOSMemory forces a garbage collection.
func (d *Debug) FreeOSMemory() (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			debug.FreeOSMemory()

			return nil, nil
		},
	)
}

// GcStats returns GC statistics.
func (d *Debug) GcStats() (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			s := new(debug.GCStats)
			debug.ReadGCStats(s)

			return s, nil
		},
	)
}

// MemStats returns detailed runtime memory statistics.
func (d *Debug) MemStats() (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			s := new(runtime.MemStats)
			runtime.ReadMemStats(s)

			return s, nil
		},
	)
}

// MutexProfile turns on mutex profiling for nsec seconds and writes profile data to file.
// It uses a profile rate of 1 for most accurate information. If a different rate is
// desired, set the rate and write the profile manually.
func (d *Debug) MutexProfile(file string, nsec int64) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			runtime.SetMutexProfileFraction(1)
			time.Sleep(time.Duration(nsec) * time.Second)
			defer runtime.SetMutexProfileFraction(0)

			return writeProfile(mutexString, file), nil
		},
	)
}

// BlockProfile turns on goroutine profiling for nsec seconds and writes profile data to
// file. It uses a profile rate of 1 for most accurate information. If a different rate is
// desired, set the rate and write the profile manually.
func (d *Debug) BlockProfile(file string, nsec int64) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			runtime.SetBlockProfileRate(1)
			time.Sleep(time.Duration(nsec) * time.Second)

			defer runtime.SetBlockProfileRate(0)

			return writeProfile(blockString, file), nil
		},
	)
}

// SetBlockProfileRate sets the rate of goroutine block profile data collection.
// rate 0 disables block profiling.
func (d *Debug) SetBlockProfileRate(rate int) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			runtime.SetBlockProfileRate(rate)

			return nil, nil
		},
	)
}

// SetGCPercent sets the garbage collection target percentage. It returns the previous
// setting. A negative value disables GC.
func (d *Debug) SetGCPercent(v int) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			return debug.SetGCPercent(v), nil
		},
	)
}

// SetMutexProfileFraction sets the rate of mutex profiling.
func (d *Debug) SetMutexProfileFraction(rate int) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			runtime.SetMutexProfileFraction(rate)

			return nil, nil
		},
	)
}

// StartCPUProfile turns on CPU profiling, writing to the given file.
func (d *Debug) StartCPUProfile(file string) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			if err := d.handler.StartCPUProfile(file); err != nil {
				return nil, err
			}

			return nil, nil
		},
	)
}

// StartGoTrace turns on tracing, writing to the given file.
func (d *Debug) StartGoTrace(file string) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			if err := d.handler.StartGoTrace(file); err != nil {
				return nil, err
			}

			return nil, nil
		},
	)
}

// StopCPUProfile stops an ongoing CPU profile.
func (d *Debug) StopCPUProfile() (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			if err := d.handler.StopCPUProfile(); err != nil {
				return nil, err
			}

			return nil, nil
		},
	)
}

// StopGoTrace stops an ongoing trace.
func (d *Debug) StopGoTrace() (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			if err := d.handler.StopGoTrace(); err != nil {
				return nil, err
			}

			return nil, nil
		},
	)
}

// WriteBlockProfile writes a goroutine blocking profile to the given file.
func (d *Debug) WriteBlockProfile(file string) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			return nil, writeProfile(blockString, file)
		},
	)
}

// WriteMemProfile writes an allocation profile to the given file.
// Note that the profiling rate cannot be set through the API,
// it must be set on the command line.
// WriteBlockProfile writes a goroutine blocking profile to the given file.
func (d *Debug) WriteMemProfile(file string) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			return nil, writeProfile(heapString, file)
		},
	)
}

// WriteMutexProfile writes a goroutine blocking profile to the given file.
func (d *Debug) WriteMutexProfile(file string) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			return nil, writeProfile(mutexString, file)
		},
	)
}

// Stacks returns a printed representation of the stacks of all goroutines. It
// also permits the following optional filters to be used:
//   - filter: boolean expression of packages to filter for
func (d *Debug) Stacks(filter *string) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			return d.handler.Stacks(filter)
		},
	)
}

type TraceConfig struct {
	EnableMemory      bool    `json:"enableMemory"`
	DisableStack      bool    `json:"disableStack"`
	DisableStorage    bool    `json:"disableStorage"`
	EnableReturnData  bool    `json:"enableReturnData"`
	DisableStructLogs bool    `json:"disableStructLogs"`
	Timeout           *string `json:"timeout"`
	Tracer            string  `json:"tracer"`
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

func (d *Debug) TraceBlockFromFile(
	input string,
	config *TraceConfig,
) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			blockByte, decodeErr := d.ReadFileFunc(input)
			if decodeErr != nil {
				return nil, fmt.Errorf("could not read file: %w", decodeErr)
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
			tx, block := GetTxAndBlockByTxHash(txHash, d.store)
			if tx == nil {
				return nil, fmt.Errorf("tx %s not found", txHash.String())
			}

			if block.Number() == 0 {
				return nil, ErrTraceGenesisBlock
			}

			tracer, cancel, err := newTracer(config)
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
	config *TraceConfig,
) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			header, err := GetHeaderFromBlockNumberOrHash(filter, d.store)
			if err != nil {
				return nil, ErrHeaderNotFound
			}

			tx, err := DecodeTxn(arg, d.store, true)
			if err != nil {
				return nil, err
			}

			// If the caller didn't supply the gas limit in the message, then we set it to maximum possible => block gas limit
			if tx.Gas() == 0 {
				tx.SetGas(header.GasLimit)
			}

			tracer, cancel, err := newTracer(config)
			if err != nil {
				return nil, err
			}

			defer cancel()

			return d.store.TraceCall(tx, header, tracer)
		},
	)
}

// GetRawBlock retrieves the RLP encoded for a single block.
func (d *Debug) GetRawBlock(filter BlockNumberOrHash) (interface{}, error) {
	return d.throttling.AttemptRequest(
		context.Background(),
		func() (interface{}, error) {
			header, err := GetHeaderFromBlockNumberOrHash(filter, d.store)
			if err != nil {
				return nil, err
			}

			block, ok := d.store.GetBlockByHash(header.Hash, true)
			if !ok {
				return nil, fmt.Errorf("block %s not found", header.Hash)
			}

			return block.MarshalRLP(), nil
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

	tracer, cancel, err := newTracer(config)
	if err != nil {
		return nil, err
	}

	defer cancel()

	return d.store.TraceBlock(block, tracer)
}

// newTracer creates new tracer by config
func newTracer(config *TraceConfig) (
	tracer.Tracer,
	context.CancelFunc,
	error,
) {
	var (
		timeout = defaultTraceTimeout
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

	var tracer tracer.Tracer

	if config.Tracer == callTracerName {
		tracer = &calltracer.CallTracer{}
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
