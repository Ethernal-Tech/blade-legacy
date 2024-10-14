package helpers

import (
	"errors"
	"math/big"

	"github.com/0xPolygon/polygon-edge/blockchain"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
)

const AbiMethodIDLength = 4

var ErrStateTransactionInputInvalid = errors.New("state transactions should have input")

// GetBlockData returns block header and extra
func GetBlockData(blockNumber uint64, blockchainBackend polychain.Blockchain) (*types.Header, *polytypes.Extra, error) {
	blockHeader, found := blockchainBackend.GetHeaderByNumber(blockNumber)
	if !found {
		return nil, nil, blockchain.ErrNoBlock
	}

	blockExtra, err := polytypes.GetIbftExtra(blockHeader.ExtraData)
	if err != nil {
		return nil, nil, err
	}

	return blockHeader, blockExtra, nil
}

// ConvertLog converts types.Log to ethgo.Log
func ConvertLog(log *types.Log) *ethgo.Log {
	l := &ethgo.Log{
		Address: ethgo.Address(log.Address),
		Data:    make([]byte, len(log.Data)),
		Topics:  make([]ethgo.Hash, len(log.Topics)),
	}

	copy(l.Data, log.Data)

	for i, topic := range log.Topics {
		l.Topics[i] = ethgo.Hash(topic)
	}

	return l
}

// CreateStateTransactionWithData creates a state transaction
// with provided target address and inputData parameter which is ABI encoded byte array.
func CreateStateTransactionWithData(target types.Address, inputData []byte) *types.Transaction {
	tx := types.NewTx(types.NewStateTx(
		types.WithGasPrice(big.NewInt(0)),
		types.WithFrom(contracts.SystemCaller),
		types.WithTo(&target),
		types.WithInput(inputData),
		types.WithGas(types.StateTransactionGasLimit),
	))

	return tx.ComputeHash()
}
