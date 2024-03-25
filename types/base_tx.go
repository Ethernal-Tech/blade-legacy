package types

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/valyala/fastjson"
)

// BaseTx represents a base abstract transaction in the blockchain,
// that is inhereted by all other concrete tx types (legacy, dynamic, acl, state).
type BaseTx struct {
	Nonce   uint64   // Nonce is the transaction nonce.
	Gas     uint64   // Gas is the amount of gas for the transaction.
	To      *Address // To is the recipient address of the transaction.
	Value   *big.Int // Value is the amount of value to be transferred.
	Input   []byte   // Input is the transaction input data.
	V, R, S *big.Int // V, R, S are the signature values of the transaction.
	Hash    Hash     // Hash is the hash of the transaction.
	From    Address  // From is the sender address of the transaction.
}

func (tx *BaseTx) nonce() uint64   { return tx.Nonce }
func (tx *BaseTx) gas() uint64     { return tx.Gas }
func (tx *BaseTx) to() *Address    { return tx.To }
func (tx *BaseTx) value() *big.Int { return tx.Value }
func (tx *BaseTx) input() []byte   { return tx.Input }
func (tx *BaseTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}
func (tx *BaseTx) hash() Hash    { return tx.Hash }
func (tx *BaseTx) from() Address { return tx.From }
func (tx *BaseTx) v() *big.Int   { return tx.V }

func (tx *BaseTx) setNonce(nonce uint64) {
	tx.Nonce = nonce
}

func (tx *BaseTx) setGas(gas uint64) {
	tx.Gas = gas
}

func (tx *BaseTx) setTo(address *Address) {
	tx.To = address
}

func (tx *BaseTx) setValue(value *big.Int) {
	tx.Value = value
}

func (tx *BaseTx) setInput(input []byte) {
	tx.Input = input
}

func (tx *BaseTx) setSignatureValues(v, r, s *big.Int) {
	tx.V, tx.R, tx.S = v, r, s
}

func (tx *BaseTx) setHash(h Hash) {
	tx.Hash = h
}

func (tx *BaseTx) setFrom(address Address) {
	tx.From = address
}

func (tx *BaseTx) copy() *BaseTx {
	cpy := new(BaseTx)

	cpy.setNonce(tx.nonce())
	cpy.setGas(tx.gas())
	cpy.setTo(tx.to())

	if tx.value() != nil {
		value := new(big.Int)
		value.Set(tx.value())

		cpy.setValue(value)
	}

	inputCopy := make([]byte, len(tx.input()))
	copy(inputCopy, tx.input()[:])

	cpy.setInput(inputCopy)

	v, r, s := tx.rawSignatureValues()

	var vCopy, rCopy, sCopy *big.Int

	if v != nil {
		vCopy = new(big.Int)
		vCopy.Set(v)
	}

	if r != nil {
		rCopy = new(big.Int)
		rCopy.Set(r)
	}

	if s != nil {
		sCopy = new(big.Int)
		sCopy.Set(s)
	}

	cpy.setSignatureValues(vCopy, rCopy, sCopy)
	cpy.setHash(tx.hash())
	cpy.setFrom(tx.from())

	return cpy
}

func (tx *BaseTx) marshalJSON(a *fastjson.Arena, v *fastjson.Value) {
	v.Set("hash", a.NewString(tx.Hash.String()))
	v.Set("from", a.NewString(tx.From.String()))
	v.Set("gas", a.NewString(fmt.Sprintf("0x%x", tx.Gas)))
	v.Set("nonce", a.NewString(fmt.Sprintf("0x%x", tx.Nonce)))

	if tx.V != nil {
		v.Set("v", a.NewString(hex.EncodeToHex(tx.V.Bytes())))
	}

	if tx.R != nil {
		v.Set("r", a.NewString(hex.EncodeToHex(tx.R.Bytes())))
	}

	if tx.S != nil {
		v.Set("s", a.NewString(hex.EncodeToHex(tx.S.Bytes())))
	}

	if len(tx.Input) != 0 {
		v.Set("input", a.NewString(hex.EncodeToHex(tx.Input)))
	}

	if tx.Value != nil {
		v.Set("value", a.NewString(fmt.Sprintf("0x%x", tx.Value)))
	}

	if tx.To == nil {
		v.Set("to", a.NewNull())
	} else {
		v.Set("to", a.NewString(tx.To.String()))
	}
}

func (tx *BaseTx) unmarshalJSON(v *fastjson.Value) error {
	hash, err := UnmarshalJSONHash(v, "hash")
	if err != nil {
		return err
	}

	tx.setHash(hash)

	from, err := UnmarshalJSONAddr(v, "from")
	if err != nil {
		return err
	}

	tx.setFrom(from)

	// Do not decode 'to' if it doesn't exist.
	if HasJSONKey(v, "to") {
		if v.Get("to").String() != "null" {
			var to Address

			if to, err = UnmarshalJSONAddr(v, "to"); err != nil {
				return err
			}

			tx.setTo(&to)
		}
	}

	input, err := UnmarshalJSONBytes(v, "input")
	if err != nil {
		return err
	}

	tx.setInput(input)

	value, err := UnmarshalJSONBigInt(v, "value")
	if err != nil {
		return err
	}

	tx.setValue(value)

	nonce, err := UnmarshalJSONUint64(v, "nonce")
	if err != nil {
		return err
	}

	tx.setNonce(nonce)

	vParity, err := UnmarshalJSONBigInt(v, "v")
	if err != nil {
		return err
	}

	r, err := UnmarshalJSONBigInt(v, "r")
	if err != nil {
		return err
	}

	s, err := UnmarshalJSONBigInt(v, "s")
	if err != nil {
		return err
	}

	tx.setSignatureValues(vParity, r, s)

	gas, err := UnmarshalJSONUint64(v, "gas")
	if err != nil {
		return err
	}

	tx.setGas(gas)

	return nil
}
