package crypto

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/0xPolygon/polygon-edge/helper/keccak"
	"github.com/0xPolygon/polygon-edge/types"
)

// EIP155Signer may be used for signing legacy (pre-EIP-155 and EIP-155) transactions
type EIP155Signer struct {
	HomesteadSigner
	chainID uint64
}

// NewEIP155Signer returns new EIP155Signer object (constructor)
//
// EIP155Signer accepts the following types of transactions:
//   - EIP-155 replay protected transactions, and
//   - pre-EIP-155 legacy transactions
func NewEIP155Signer(chainID uint64) *EIP155Signer {
	return &EIP155Signer{
		chainID: chainID,
	}
}

// Hash returns the keccak256 hash of the transaction
//
// The EIP-155 transaction hash preimage is as follows:
// RLP(nonce, gasPrice, gas, to, value, input, chainId, 0, 0)
//
// Specification: https://eips.ethereum.org/EIPS/eip-155#specification
func (signer *EIP155Signer) Hash(tx *types.Transaction) types.Hash {
	if tx.ChainID().Cmp(big.NewInt(0)) == 0 {
		return signer.HomesteadSigner.Hash(tx)
	}

	var hash []byte

	RLP := arenaPool.Get()

	// RLP(-, -, -, -, -, -, -, -, -)
	hashPreimage := RLP.NewArray()

	// RLP(nonce, -, -, -, -, -, -, -, -)
	hashPreimage.Set(RLP.NewUint(tx.Nonce()))

	// RLP(nonce, gasPrice, -, -, -, -, -, -, -)
	hashPreimage.Set(RLP.NewBigInt(tx.GasPrice()))

	// RLP(nonce, gasPrice, gas, -, -, -, -, -, -)
	hashPreimage.Set(RLP.NewUint(tx.Gas()))

	// Checking whether the transaction is a smart contract deployment
	if tx.To() == nil {
		// RLP(nonce, gasPrice, gas, to, -, -, -, -, -)
		hashPreimage.Set(RLP.NewNull())
	} else {
		// RLP(nonce, gasPrice, gas, to, -, -, -, -, -)
		hashPreimage.Set(RLP.NewCopyBytes((*(tx.To())).Bytes()))
	}

	// RLP(nonce, gasPrice, gas, to, value, -, -, -, -)
	hashPreimage.Set(RLP.NewBigInt(tx.Value()))

	// RLP(nonce, gasPrice, gas, to, value, input, -, -, -)
	hashPreimage.Set(RLP.NewCopyBytes(tx.Input()))

	// RLP(nonce, gasPrice, gas, to, value, input, chainId, -, -)
	hashPreimage.Set(RLP.NewUint(signer.chainID))

	// RLP(nonce, gasPrice, gas, to, value, input, chainId, 0, -)
	hashPreimage.Set(RLP.NewUint(0))

	// RLP(nonce, gasPrice, gas, to, value, input, chainId, 0, 0)
	hashPreimage.Set(RLP.NewUint(0))

	// keccak256(RLP(nonce, gasPrice, gas, to, value, input))
	hash = keccak.Keccak256Rlp(nil, hashPreimage)

	arenaPool.Put(RLP)

	return types.BytesToHash(hash)
}

// Sender returns the sender of the transaction
func (signer *EIP155Signer) Sender(tx *types.Transaction) (types.Address, error) {
	if tx.ChainID().Cmp(big.NewInt(0)) == 0 {
		return signer.HomesteadSigner.Sender(tx)
	}

	v, r, s := tx.RawSignatureValues()

	// Checking one of the values is enought since they are inseparable
	if v == nil {
		return types.Address{}, errors.New("Sender method: Unknown signature")
	}

	// Reverse the V calculation to find the parity of the Y coordinate
	// v = CHAIN_ID * 2 + 35 + {0, 1} -> {0, 1} = v - 35 - CHAIN_ID * 2

	a := big.NewInt(0)
	b := big.NewInt(0)
	parity := big.NewInt(0)

	// a = v - 35
	a.Sub(v, big35)

	// b = CHAIN_ID * 2
	b.Mul(big.NewInt(int64(signer.chainID)), big.NewInt(2))

	// parity = a - b
	parity.Sub(a, b)

	return recoverAddress(signer.Hash(tx), r, s, parity, true)
}

// SingTx takes the original transaction as input and returns its signed version
func (signer *EIP155Signer) SignTx(tx *types.Transaction, privateKey *ecdsa.PrivateKey) (*types.Transaction, error) {
	if tx.ChainID().Cmp(big.NewInt(0)) == 0 {
		return signer.HomesteadSigner.SignTx(tx, privateKey)
	}

	tx = tx.Copy()

	hash := signer.Hash(tx)

	signature, err := Sign(privateKey, hash[:])
	if err != nil {
		return nil, err
	}

	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])

	if s.Cmp(secp256k1NHalf) > 0 {
		return nil, errors.New("SignTx method: S must be inclusively lower than secp256k1n/2")
	}

	v := new(big.Int).SetBytes(signer.calculateV(signature[64]))

	tx.SetSignatureValues(v, r, s)

	return tx, nil
}

// Private method calculateV returns the V value for the EIP-155 transactions
//
// V is calculated by the formula: {0, 1} + CHAIN_ID * 2 + 35 where {0, 1} denotes the parity of the Y coordinate
func (signer *EIP155Signer) calculateV(parity byte) []byte {
	a := big.NewInt(0)
	b := big.NewInt(0)
	result := big.NewInt(0)

	// a = {0, 1} + 35
	a.Add(big.NewInt(int64(parity)), big35)

	// b = CHAIN_ID * 2
	b.Mul(big.NewInt(int64(signer.chainID)), big.NewInt(2))

	// result = a + b
	result.Add(a, b)

	return result.Bytes()
}
