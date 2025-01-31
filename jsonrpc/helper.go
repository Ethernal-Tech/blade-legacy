package jsonrpc

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/user"
	"path/filepath"
	"runtime/pprof"
	"strings"

	"github.com/0xPolygon/polygon-edge/types"
)

var (
	ErrHeaderNotFound           = errors.New("header not found")
	ErrLatestNotFound           = errors.New("latest header not found")
	ErrNegativeBlockNumber      = errors.New("invalid argument 0: block number must not be negative")
	ErrFailedFetchGenesis       = errors.New("error fetching genesis block header")
	ErrNoDataInContractCreation = errors.New("contract creation without data provided")
	ErrIndexOutOfRange          = errors.New("the index is invalid, it is out of range of expected values")
	ErrInsufficientFunds        = errors.New("insufficient funds for execution")
)

type latestHeaderGetter interface {
	Header() *types.Header
}

// GetNumericBlockNumber returns block number based on current state or specified number
func GetNumericBlockNumber(number BlockNumber, store latestHeaderGetter) (uint64, error) {
	switch number {
	case SafeBlockNumber:
		fallthrough
	case FinalizedBlockNumber:
		fallthrough
	case LatestBlockNumber:
		fallthrough
	case PendingBlockNumber:
		latest := store.Header()
		if latest == nil {
			return 0, ErrLatestNotFound
		}

		return latest.Number, nil

	case EarliestBlockNumber:
		return 0, nil

	default:
		if number < 0 {
			return 0, ErrNegativeBlockNumber
		}

		return uint64(number), nil
	}
}

// GetTransactionByBlockAndIndex returns the transaction for the given block and index.
func GetTransactionByBlockAndIndex(block *types.Block, index argUint64) (interface{}, error) {
	idx := int(index)
	size := len(block.Transactions)

	if size == 0 || size < idx {
		return nil, ErrIndexOutOfRange
	}

	return toTransaction(
		block.Transactions[index],
		block.Header,
		&idx,
	), nil
}

type headerGetter interface {
	Header() *types.Header
	GetHeaderByNumber(uint64) (*types.Header, bool)
}

// GetBlockHeader returns a header using the provided number
func GetBlockHeader(number BlockNumber, store headerGetter) (*types.Header, error) {
	switch number {
	case SafeBlockNumber:
		fallthrough
	case FinalizedBlockNumber:
		fallthrough
	case LatestBlockNumber:
		fallthrough
	case PendingBlockNumber:
		return store.Header(), nil

	case EarliestBlockNumber:
		header, ok := store.GetHeaderByNumber(uint64(0))
		if !ok {
			return nil, ErrFailedFetchGenesis
		}

		return header, nil

	default:
		// Convert the block number from hex to uint64
		header, ok := store.GetHeaderByNumber(uint64(number))
		if !ok {
			return nil, fmt.Errorf("error fetching block number %d header", uint64(number))
		}

		return header, nil
	}
}

type txLookupAndBlockGetter interface {
	ReadTxLookup(types.Hash) (uint64, bool)
	GetBlockByNumber(uint64, bool) (*types.Block, bool)
}

// GetTxAndBlockByTxHash returns the tx and the block including the tx by given tx hash
func GetTxAndBlockByTxHash(txHash types.Hash, store txLookupAndBlockGetter) (*types.Transaction, *types.Block) {
	blockNum, ok := store.ReadTxLookup(txHash)
	if !ok {
		return nil, nil
	}

	block, ok := store.GetBlockByNumber(blockNum, true)
	if !ok {
		return nil, nil
	}

	if txn, _ := types.FindTxByHash(block.Transactions, txHash); txn != nil {
		return txn, block
	}

	return nil, nil
}

type blockGetter interface {
	Header() *types.Header
	GetHeaderByNumber(uint64) (*types.Header, bool)
	GetBlockByHash(types.Hash, bool) (*types.Block, bool)
}

func GetHeaderFromBlockNumberOrHash(bnh BlockNumberOrHash, store blockGetter) (*types.Header, error) {
	// The filter is empty, use the latest block by default
	if bnh.BlockNumber == nil && bnh.BlockHash == nil {
		bnh.BlockNumber, _ = createBlockNumberPointer(latest)
	}

	if bnh.BlockNumber != nil {
		// block number
		header, err := GetBlockHeader(*bnh.BlockNumber, store)
		if err != nil {
			return nil, fmt.Errorf("failed to get the header of block %d: %w", *bnh.BlockNumber, err)
		}

		return header, nil
	}

	// block hash
	block, ok := store.GetBlockByHash(*bnh.BlockHash, false)
	if !ok {
		return nil, fmt.Errorf("could not find block referenced by the hash %s", bnh.BlockHash.String())
	}

	return block.Header, nil
}

type nonceGetter interface {
	Header() *types.Header
	GetHeaderByNumber(uint64) (*types.Header, bool)
	GetNonce(types.Address) uint64
	GetAccount(root types.Hash, addr types.Address) (*Account, error)
}

func GetNextNonce(address types.Address, number BlockNumber, store nonceGetter) (uint64, error) {
	if number == PendingBlockNumber {
		// Grab the latest pending nonce from the TxPool
		// If the account is not initialized in the local TxPool,
		// return the latest nonce from the world state
		return store.GetNonce(address), nil
	}

	header, err := GetBlockHeader(number, store)
	if err != nil {
		return 0, err
	}

	acc, err := store.GetAccount(header.StateRoot, address)

	//nolint:govet
	if errors.Is(err, ErrStateNotFound) {
		// If the account doesn't exist / isn't initialized,
		// return a nonce value of 0
		return 0, nil
	} else if err != nil {
		return 0, err
	}

	return acc.Nonce, nil
}

func DecodeTxn(arg *txnArgs, store nonceGetter, forceSetNonce bool) (*types.Transaction, error) {
	if arg == nil {
		return nil, errors.New("missing value for required argument 0")
	}
	// set default values
	if arg.From == nil {
		arg.From = &types.ZeroAddress
		arg.Nonce = argUintPtr(0)
	} else if arg.Nonce == nil || forceSetNonce {
		// get nonce from the pool
		nonce, err := GetNextNonce(*arg.From, LatestBlockNumber, store)
		if err != nil {
			return nil, err
		}

		arg.Nonce = argUintPtr(nonce)
	}

	if arg.Value == nil {
		arg.Value = argBytesPtr([]byte{})
	}

	if arg.GasPrice == nil {
		arg.GasPrice = argBytesPtr([]byte{})
	}

	if arg.GasTipCap == nil {
		arg.GasTipCap = argBytesPtr([]byte{})
	}

	if arg.GasFeeCap == nil {
		arg.GasFeeCap = argBytesPtr([]byte{})
	}

	var input []byte
	if arg.Data != nil {
		input = *arg.Data
	} else if arg.Input != nil {
		input = *arg.Input
	}

	if arg.To == nil && input == nil {
		return nil, ErrNoDataInContractCreation
	}

	if input == nil {
		input = []byte{}
	}

	if arg.Gas == nil {
		arg.Gas = argUintPtr(0)
	}

	txType := types.LegacyTxType
	if arg.Type != nil {
		txType = types.TxType(*arg.Type)
	}

	txn := types.NewTxWithType(txType)

	if arg.AccessList != nil {
		txn.SetAccessList(*arg.AccessList)
	}

	if arg.ChainID != nil {
		txn.SetChainID(new(big.Int).SetUint64(uint64(*arg.ChainID)))
	}

	switch txType {
	case types.LegacyTxType:
		txn.SetGasPrice(new(big.Int).SetBytes(*arg.GasPrice))
	case types.DynamicFeeTxType:
		txn.SetGasTipCap(new(big.Int).SetBytes(*arg.GasTipCap))
		txn.SetGasFeeCap(new(big.Int).SetBytes(*arg.GasFeeCap))
	}

	txn.SetFrom(*arg.From)
	txn.SetGas(uint64(*arg.Gas))
	txn.SetValue(new(big.Int).SetBytes(*arg.Value))
	txn.SetInput(input)
	txn.SetNonce(uint64(*arg.Nonce))

	if arg.To != nil {
		txn.SetTo(arg.To)
	}

	txn.ComputeHash()

	return txn, nil
}

// expandHomeDirectory expands home directory in file paths and sanitizes it.
// For example ~someuser/tmp will not be expanded.
func expandHomeDirectory(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		home := os.Getenv("HOME")
		if home == "" {
			if usr, err := user.Current(); err == nil {
				home = usr.HomeDir
			}
		}

		if home != "" {
			p = home + p[1:]
		}
	}

	return filepath.Clean(p)
}

func writeProfile(name, file string) error {
	p := pprof.Lookup(name)

	f, err := os.Create(expandHomeDirectory(file))
	if err != nil {
		return err
	}
	defer f.Close()

	return p.WriteTo(f, 0)
}
