package crypto

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/0xPolygon/polygon-edge/helper/keccak"
	"github.com/0xPolygon/polygon-edge/types"
)

// FrontierSigner may be used for pre-EIP-155 transactions
type FrontierSigner struct {
}

// NewFrontierSigner returns new FrontierSigner object (constructor)
//
// FrontierSigner accepts the following types of transactions:
//   - pre-EIP-155 transactions
func NewFrontierSigner() *FrontierSigner {
	return &FrontierSigner{}
}

// Hash returns the keccak256 hash of the transaction
//
// The pre-EIP-155 transaction hash preimage is as follows:
// RLP(nonce, gasPrice, gas, to, value, input)
//
// Specification: https://eips.ethereum.org/EIPS/eip-155#specification
func (signer *FrontierSigner) Hash(tx *types.Transaction) types.Hash {
	RLP := arenaPool.Get()
	defer arenaPool.Put(RLP)

	// RLP(-, -, -, -, -, -)
	hashPreimage := RLP.NewArray()

	// RLP(nonce, -, -, -, -, -)
	hashPreimage.Set(RLP.NewUint(tx.Nonce()))

	// RLP(nonce, gasPrice, -, -, -, -)
	hashPreimage.Set(RLP.NewBigInt(tx.GasPrice()))

	// RLP(nonce, gasPrice, gas, -, -, -)
	hashPreimage.Set(RLP.NewUint(tx.Gas()))

	// Checking whether the transaction is a smart contract deployment
	if tx.To() == nil {
		// RLP(nonce, gasPrice, gas, to, -, -)
		hashPreimage.Set(RLP.NewNull())
	} else {
		// RLP(nonce, gasPrice, gas, to, -, -)
		hashPreimage.Set(RLP.NewCopyBytes((*(tx.To())).Bytes()))
	}

	// RLP(nonce, gasPrice, gas, to, value, -)
	hashPreimage.Set(RLP.NewBigInt(tx.Value()))

	// RLP(nonce, gasPrice, gas, to, value, input)
	hashPreimage.Set(RLP.NewCopyBytes(tx.Input()))

	// keccak256(RLP(nonce, gasPrice, gas, to, value, input))
	hash := keccak.Keccak256Rlp(nil, hashPreimage)

	return types.BytesToHash(hash)
}

// Sender returns the sender of the transaction
func (signer *FrontierSigner) Sender(tx *types.Transaction) (types.Address, error) {
	if tx.Type() != types.LegacyTx && tx.Type() != types.StateTx {
		return types.ZeroAddress, types.ErrTxTypeNotSupported
	}

	v, r, s := tx.RawSignatureValues()

	// Checking one of the values is enought since they are inseparable
	if v == nil {
		return types.Address{}, errors.New("Sender method: Unknown signature")
	}

	// Reverse the V calculation to find the parity of the Y coordinate
	// v = {0, 1} + 27 -> {0, 1} = v - 27
	parity := big.NewInt(0).Sub(v, big27)

	return recoverAddress(signer.Hash(tx), r, s, parity, false)
}

// SingTx takes the original transaction as input and returns its signed version
func (signer *FrontierSigner) SignTx(tx *types.Transaction, privateKey *ecdsa.PrivateKey) (*types.Transaction, error) {
	if tx.Type() != types.LegacyTx && tx.Type() != types.StateTx {
		return nil, types.ErrTxTypeNotSupported
	}

	tx = tx.Copy()

	hash := signer.Hash(tx)

	signature, err := Sign(privateKey, hash[:])
	if err != nil {
		return nil, err
	}

	r := new(big.Int).SetBytes(signature[:types.HashLength])
	s := new(big.Int).SetBytes(signature[types.HashLength : 2*types.HashLength])
	v := new(big.Int).SetBytes(signer.calculateV(signature[64]))

	tx.SetSignatureValues(v, r, s)

	return tx, nil
}

// Private method calculateV returns the V value for the pre-EIP-155 transactions
//
// V is calculated by the formula: {0, 1} + 27 where {0, 1} denotes the parity of the Y coordinate
func (signer *FrontierSigner) calculateV(parity byte) []byte {
	result := big.NewInt(0)

	// result = {0, 1} + 27
	result.Add(big.NewInt(int64(parity)), big27)

	return result.Bytes()
}
