package precompiled

import (
	"encoding/hex"
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
		str, ok := data[strconv.Itoa(i)].(string)
		if !ok {
			return nil, runtime.ErrInvalidInputData
		}

		dataBytes[i], err = hex.DecodeString(strings.TrimPrefix(str, "0x"))
		if err != nil {
			// if not hex encoded then its just a plain string -> convert it to byte slice
			dataBytes[i] = []byte(str)
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

	// golang unexpected behaviour fix?!?!
	// `switch cardano_wallet.VerifyMessage(message, verifyingKey, signature)` acts strange
	switch err = cardano_wallet.VerifyMessage(rawTxOrMessage, verifyingKey, signature); err { //nolint:errorlint
	case nil:
		return abiBoolTrue, nil
	case cardano_wallet.ErrInvalidSignature:
		return abiBoolFalse, nil
	default:
		return nil, err
	}
}
