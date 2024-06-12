package precompiled

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/types"
	cardano_indexer "github.com/Ethernal-Tech/cardano-infrastructure/indexer"
	cardano_wallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/umbracle/ethgo/abi"
)

var cardanoVerifySignaturePrecompileInputABIType = abi.MustNewType("tuple(string, string, string, bool)")

// cardanoVerifySignaturePrecompile is a concrete implementation of the contract interface.
type cardanoVerifySignaturePrecompile struct {
}

// gas returns the gas required to execute the pre-compiled contract
func (c *cardanoVerifySignaturePrecompile) gas(_ []byte, config *chain.ForksInTime) uint64 {
	return 150000
}

// Run runs the precompiled contract with the given input.
// isValidSignature(string rawTxOrMessage, string signature, string verifyingKey, bool isTx):
// Output could be an error or ABI encoded "bool" value
func (c *cardanoVerifySignaturePrecompile) run(input []byte, caller types.Address, _ runtime.Host) ([]byte, error) {
	rawData, err := abi.Decode(cardanoVerifySignaturePrecompileInputABIType, input)
	if err != nil {
		return nil, err
	}

	data, ok := rawData.(map[string]interface{})
	if !ok || len(data) != 4 {
		return nil, runtime.ErrInvalidInputData
	}

	dataBytes := [3][]byte{}

	for i := range dataBytes {
		switch dt := data[strconv.Itoa(i)].(type) {
		case string:
			dataBytes[i], err = hex.DecodeString(strings.TrimPrefix(dt, "0x"))
			if err != nil {
				dataBytes[i] = []byte(dt)
			}
		case []byte:
			dataBytes[i] = dt
		case [32]byte:
			dataBytes[i] = dt[:]
		default:
			return nil, fmt.Errorf("%w: index %d", runtime.ErrInvalidInputData, i)
		}
	}

	rawTxOrMessage, witnessOrSignature, verifyingKey := dataBytes[0], dataBytes[1], dataBytes[2]

	isTx, ok := data["3"].(bool)
	if !ok {
		return nil, runtime.ErrInvalidInputData
	}

	// second parameter can be witness
	signature, _, err := cardano_wallet.TxWitnessRaw(witnessOrSignature).GetSignatureAndVKey()
	if err != nil {
		signature = witnessOrSignature
	}

	// if first argument is raw transaction we need to get tx hash from it
	if isTx {
		txInfo, err := cardano_indexer.ParseTxInfo(rawTxOrMessage)
		if err != nil {
			return nil, err
		}

		rawTxOrMessage, err = hex.DecodeString(txInfo.Hash)
		if err != nil {
			return nil, err
		}
	}

	err = cardano_wallet.VerifyMessage(rawTxOrMessage, verifyingKey, signature)
	if err != nil {
		if errors.Is(err, cardano_wallet.ErrInvalidSignature) {
			return abiBoolFalse, nil
		}

		return nil, err
	}

	return abiBoolTrue, nil
}
