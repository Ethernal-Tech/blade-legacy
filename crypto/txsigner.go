package crypto

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/helper/keccak"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/fastrlp"
)

// Magic numbers, taken from the Ethereum, used in the calculation of the V value
// Only matters in pre-EIP-2930 (pre-Berlin) transactions
var (
	big27 = big.NewInt(27) // pre-EIP-155
	big35 = big.NewInt(35) // EIP-155
)

// RLP encoding helper
var arenaPool fastrlp.ArenaPool

// TxSigner is a utility interface used to work with transaction signatures
type TxSigner interface {
	// Hash returns the hash of the transaction
	Hash(*types.Transaction) types.Hash

	// Sender returns the sender of the transaction
	Sender(*types.Transaction) (types.Address, error)

	// SingTx takes the original transaction as input and returns its signed version
	SignTx(*types.Transaction, *ecdsa.PrivateKey) (*types.Transaction, error)
}

// NewSigner creates a new signer based on currently supported forks
func NewSigner(forks chain.ForksInTime, chainID uint64) TxSigner {
	var signer TxSigner

	if forks.London {
		signer = NewLondonSigner(chainID)
	} else if forks.Berlin {
		signer = NewBerlinSigner(chainID)
	} else if forks.EIP155 {
		signer = NewEIP155Signer(chainID)
	} else if forks.Homestead {
		signer = NewHomesteadSigner()
	} else {
		signer = NewFrontierSigner()
	}

	return signer
}

// encodeSignature generates a signature based on the R, S and parity values
//
// The signature encoding format is as follows:
// (32-bytes R, 32-bytes S, 1-byte parity)
//
// Note: although the signature value V, based on different standards, is calculated and encoded in different ways,
// the encodeSignature function expects parity of Y coordinate as third input and that is what will be encoded
func encodeSignature(r, s, parity *big.Int, isHomestead bool) ([]byte, error) {
	if !ValidateSignatureValues(parity, r, s, isHomestead) {
		return nil, errors.New("Signature encoding failed: Invalid transaction signature")
	}

	signature := make([]byte, 65)

	copy(signature[32-len(r.Bytes()):32], r.Bytes())
	copy(signature[64-len(s.Bytes()):64], s.Bytes())
	signature[64] = byte(parity.Int64())

	return signature, nil
}

// recoverAddress recovers the sender address from the transaction hash and signature R, S and parity values
func recoverAddress(txHash types.Hash, r, s, parity *big.Int, isHomestead bool) (types.Address, error) {
	signature, err := encodeSignature(r, s, parity, isHomestead)
	if err != nil {
		return types.Address{}, err
	}

	publicKey, err := Ecrecover(txHash.Bytes(), signature)
	if err != nil {
		return types.Address{}, err
	}

	// First byte of the publicKey indicates that it is serialized in uncompressed form (it has the value 0x04), so we ommit that
	hash := Keccak256(publicKey[1:])

	address := hash[12:]

	return types.BytesToAddress(address), nil
}

// calcTxHash calculates the transaction hash (keccak256 hash of the RLP value)
// LegacyTx:
// keccak256(RLP(nonce, gasPrice, gas, to, value, input, chainId, 0, 0))
// AccessListsTx:
// keccak256(RLP(type, chainId, nonce, gasPrice, gas, to, value, input, accessList))
// DynamicFeeTx:
// keccak256(RLP(type, chainId, nonce, gasTipCap, gasFeeCap, gas, to, value, input, accessList))
func calcTxHash(tx *types.Transaction, chainID uint64) types.Hash {
	var hash []byte

	switch tx.Type() {
	case types.AccessListTx:
		a := arenaPool.Get()
		v := a.NewArray()

		v.Set(a.NewUint(chainID))
		v.Set(a.NewUint(tx.Nonce()))
		v.Set(a.NewBigInt(tx.GasPrice()))
		v.Set(a.NewUint(tx.Gas()))

		if tx.To() == nil {
			v.Set(a.NewNull())
		} else {
			v.Set(a.NewCopyBytes((*(tx.To())).Bytes()))
		}

		v.Set(a.NewBigInt(tx.Value()))
		v.Set(a.NewCopyBytes(tx.Input()))

		// add accessList
		accessListVV := a.NewArray()

		if tx.AccessList() != nil {
			for _, accessTuple := range tx.AccessList() {
				accessTupleVV := a.NewArray()
				accessTupleVV.Set(a.NewCopyBytes(accessTuple.Address.Bytes()))

				storageKeysVV := a.NewArray()
				for _, storageKey := range accessTuple.StorageKeys {
					storageKeysVV.Set(a.NewCopyBytes(storageKey.Bytes()))
				}

				accessTupleVV.Set(storageKeysVV)
				accessListVV.Set(accessTupleVV)
			}
		}

		v.Set(accessListVV)

		hash = keccak.PrefixedKeccak256Rlp([]byte{byte(tx.Type())}, nil, v)

		arenaPool.Put(a)

		return types.BytesToHash(hash)

	case types.DynamicFeeTx, types.LegacyTx, types.StateTx:
		a := arenaPool.Get()
		isDynamicFeeTx := tx.Type() == types.DynamicFeeTx

		v := a.NewArray()

		if isDynamicFeeTx {
			v.Set(a.NewUint(chainID))
		}

		v.Set(a.NewUint(tx.Nonce()))

		if isDynamicFeeTx {
			v.Set(a.NewBigInt(tx.GasTipCap()))
			v.Set(a.NewBigInt(tx.GasFeeCap()))
		} else {
			v.Set(a.NewBigInt(tx.GasPrice()))
		}

		v.Set(a.NewUint(tx.Gas()))

		if tx.To() == nil {
			v.Set(a.NewNull())
		} else {
			v.Set(a.NewCopyBytes((*(tx.To())).Bytes()))
		}

		v.Set(a.NewBigInt(tx.Value()))

		v.Set(a.NewCopyBytes(tx.Input()))

		if isDynamicFeeTx {
			// Convert TxAccessList to RLP format and add it to the vv array.
			accessListVV := a.NewArray()

			if tx.AccessList() != nil {
				for _, accessTuple := range tx.AccessList() {
					accessTupleVV := a.NewArray()
					accessTupleVV.Set(a.NewCopyBytes(accessTuple.Address.Bytes()))

					storageKeysVV := a.NewArray()
					for _, storageKey := range accessTuple.StorageKeys {
						storageKeysVV.Set(a.NewCopyBytes(storageKey.Bytes()))
					}

					accessTupleVV.Set(storageKeysVV)
					accessListVV.Set(accessTupleVV)
				}
			}

			v.Set(accessListVV)
		} else {
			// EIP155
			if chainID != 0 {
				v.Set(a.NewUint(chainID))
				v.Set(a.NewUint(0))
				v.Set(a.NewUint(0))
			}
		}

		if isDynamicFeeTx {
			hash = keccak.PrefixedKeccak256Rlp([]byte{byte(tx.Type())}, nil, v)
		} else {
			hash = keccak.Keccak256Rlp(nil, v)
		}

		arenaPool.Put(a)
	}

	return types.BytesToHash(hash)
}
