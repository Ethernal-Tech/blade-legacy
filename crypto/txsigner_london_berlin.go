package crypto

import (
	"crypto/ecdsa"
	"math/big"

	"github.com/0xPolygon/polygon-edge/types"
)

// LondonOrBerlinSigner implements signer for london and berlin hard forks
type LondonOrBerlinSigner struct {
	chainID        uint64
	isHomestead    bool
	fallbackSigner TxSigner
}

// NewLondonSigner returns new LondonSigner object that accepts
// - EIP-1559 dynamic fee transactions
// - EIP-2930 access list transactions,
// - EIP-155 replay protected transactions, and
// - legacy Homestead transactions.
func NewLondonSigner(chainID uint64, isHomestead bool, fallbackSigner TxSigner) *LondonOrBerlinSigner {
	return &LondonOrBerlinSigner{
		chainID:        chainID,
		isHomestead:    isHomestead,
		fallbackSigner: fallbackSigner,
	}
}

// NewBerlinSigner returns new BerlinSigner object that accepts
// - EIP-2930 access list transactions,
// - EIP-155 replay protected transactions, and
// - legacy Homestead transactions.
func NewBerlinSigner(chainID uint64, isHomestead bool, fallbackSigner TxSigner) *LondonOrBerlinSigner {
	return &LondonOrBerlinSigner{
		chainID:        chainID,
		isHomestead:    isHomestead,
		fallbackSigner: fallbackSigner,
	}
}

// Hash is a wrapper function that calls calcTxHash with the LondonSigner's fields
func (e *LondonOrBerlinSigner) Hash(tx *types.Transaction) types.Hash {
	return calcTxHash(tx, e.chainID)
}

// Sender returns the transaction sender
func (e *LondonOrBerlinSigner) Sender(tx *types.Transaction) (types.Address, error) {
	// Apply fallback signer for non-dynamic-fee-txs
	if tx.Type() != types.DynamicFeeTx {
		return e.fallbackSigner.Sender(tx)
	}

	v, r, s := tx.RawSignatureValues()
	if tx.Type() == types.AccessListTx {
		// AL txs are defined to use 0 and 1 as their recovery
		// id, add 27 to become equivalent to unprotected Homestead signatures.
		v = new(big.Int).Add(v, big.NewInt(27))
	}

	return recoverAddress(e.Hash(tx), r, s, v, e.isHomestead)
}

// SignTx signs the transaction using the passed in private key
func (e *LondonOrBerlinSigner) SignTx(tx *types.Transaction, pk *ecdsa.PrivateKey) (*types.Transaction, error) {
	// Apply fallback signer for non-dynamic-fee-txs
	if tx.Type() != types.DynamicFeeTx {
		return e.fallbackSigner.SignTx(tx, pk)
	}

	tx = tx.Copy()

	h := e.Hash(tx)

	sig, err := Sign(pk, h[:])
	if err != nil {
		return nil, err
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := new(big.Int).SetBytes(e.calculateV(sig[64]))
	tx.SetSignatureValues(v, r, s)

	return tx, nil
}

// calculateV returns the V value for transaction signatures. Based on EIP155
func (e *LondonOrBerlinSigner) calculateV(parity byte) []byte {
	return big.NewInt(int64(parity)).Bytes()
}
