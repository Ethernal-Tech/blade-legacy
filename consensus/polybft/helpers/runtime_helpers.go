package helpers

import (
	"github.com/0xPolygon/polygon-edge/blockchain"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
)

const AbiMethodIDLength = 4

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
