package crypto

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/0xPolygon/polygon-edge/helper/keccak"
	"github.com/0xPolygon/polygon-edge/types"
)

// StateSigner may be used for state transactions
type StateSigner struct {
}

// NewStateSigner returns new StateSigner object (constructor)
//
// FrontierSigner accepts the following types of transactions:
//   - state transactions
func NewStateSigner() *StateSigner {
	return &StateSigner{}
}

// Hash returns the keccak256 hash of the transaction
//
// The state transaction hash preimage is as follows:
// RLP(nonce, gasPrice, gas, to, value, input)
//
// Specification: -
func (signer *StateSigner) Hash(tx *types.Transaction) types.Hash {
	if tx.Type() != types.StateTx {
		return types.BytesToHash(make([]byte, 0))
	}

	var hash []byte

	RLP := arenaPool.Get()

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
	hash = keccak.Keccak256Rlp(nil, hashPreimage)

	arenaPool.Put(RLP)

	return types.BytesToHash(hash)
}

// Sender returns the sender of the transaction
func (signer *StateSigner) Sender(tx *types.Transaction) (types.Address, error) {
	if tx.Type() != types.StateTx {
		return types.Address{}, errors.New("Sender method: Unknown transaction type")
	}

	v, r, s := tx.RawSignatureValues()

	// Reverse the V calculation to find the parity of the Y coordinate
	// v = {0, 1} + 27 -> {0, 1} = v - 27

	parity := big.NewInt(0).Sub(v, big27)

	return recoverAddress(signer.Hash(tx), r, s, parity, false)
}

// SingTx takes the original transaction as input and returns its signed version
func (signer *StateSigner) SignTx(tx *types.Transaction, privateKey *ecdsa.PrivateKey) (*types.Transaction, error) {
	if tx.Type() != types.StateTx {
		return nil, errors.New("SignTx method: Unknown transaction type")
	}

	tx = tx.Copy()

	hash := signer.Hash(tx)

	signature, err := Sign(privateKey, hash[:])
	if err != nil {
		return nil, err
	}

	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	v := new(big.Int).SetBytes(signer.calculateV(signature[64]))

	tx.SetSignatureValues(v, r, s)

	return tx, nil
}

// Private method calculateV returns the V value for the pre-EIP-155 transactions
//
// V is calculated by the formula: {0, 1} + 27 where {0, 1} denotes the parity of the Y coordinate
func (signer *StateSigner) calculateV(parity byte) []byte {
	result := big.NewInt(0)

	// result = {0, 1} + 27
	result.Add(big.NewInt(int64(parity)), big27)

	return result.Bytes()
}
