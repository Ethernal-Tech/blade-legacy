package helpers

import (
	"github.com/0xPolygon/polygon-edge/blockchain"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/types"
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
