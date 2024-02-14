package crypto

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/0xPolygon/polygon-edge/types"
)

// HomesteadSigner may be used for signing pre-EIP155 transactions
type HomesteadSigner struct {
	FrontierSigner
}

// NewHomesteadSigner returns new FrontierSigner object (constructor)
//
// HomesteadSigner accepts the following types of transactions:
//   - pre-EIP-155 transactions
func NewHomesteadSigner() *HomesteadSigner {
	return &HomesteadSigner{
		FrontierSigner: FrontierSigner{},
	}
}

// Hash returns the keccak256 hash of the transaction
//
// The pre-EIP-155 transaction hash preimage is as follows:
// RLP(nonce, gasPrice, gas, to, value, input)
//
// Specification: https://eips.ethereum.org/EIPS/eip-155#specification
//
// Note: Since the hash is calculated in the same way as with FrontierSigner, this is just a wrapper method
func (signer *HomesteadSigner) Hash(tx *types.Transaction) types.Hash {
	return signer.FrontierSigner.Hash(tx)
}

// Sender returns the sender of the transaction
func (signer *HomesteadSigner) Sender(tx *types.Transaction) (types.Address, error) {
	if tx.Type() != types.LegacyTx {
		return types.Address{}, errors.New("Sender method: Unknown transaction type")
	}

	v, r, s := tx.RawSignatureValues()

	// Reverse the V calculation to find the parity of the Y coordinate
	// v = {0, 1} + 27 -> {0, 1} = v - 27

	parity := big.NewInt(0).Sub(v, big27)

	// The only difference compared to FrontierSinger.Sender() method is that the `isHomestead` flag is set to true
	// `isHomestead` flag denotes that the S value must be inclusively lower than the half of the secp256k1 curve order
	return recoverAddress(signer.Hash(tx), r, s, parity, true)
}

// SingTx takes the original transaction as input and returns its signed version
func (signer *HomesteadSigner) SignTx(tx *types.Transaction, privateKey *ecdsa.PrivateKey) (*types.Transaction, error) {
	if tx.Type() != types.LegacyTx {
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

	// Homestead hard-fork introduced the rule that the S value
	// must be inclusively lower than the half of the secp256k1 curve order
	// Specification: https://eips.ethereum.org/EIPS/eip-2#specification (2)
	if s.Cmp(secp256k1NHalf) > 0 {
		return nil, errors.New("SignTx method: S must be inclusively lower than secp256k1n/2")
	}

	v := new(big.Int).SetBytes(signer.calculateV(signature[64]))

	tx.SetSignatureValues(v, r, s)

	return tx, nil
}

// Private method calculateV returns the V value for the pre-EIP-155 transactions
//
// V is calculated by the formula: {0, 1} + 27 where {0, 1} denotes the parity of the Y coordinate
func (signer *HomesteadSigner) calculateV(parity byte) []byte {
	result := big.NewInt(0)

	// result = {0, 1} + 27
	result.Add(big.NewInt(int64(parity)), big27)

	return result.Bytes()
}
