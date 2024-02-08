package types

import (
	"fmt"
	"math/big"

	"github.com/umbracle/fastrlp"
)

type LegacyTx struct {
	Nonce    uint64
	GasPrice *big.Int
	Gas      uint64
	To       *Address
	Value    *big.Int
	Input    []byte
	V, R, S  *big.Int
	Hash     Hash
	From     Address
}

func (tx *LegacyTx) transactionType() TxType { return LegacyTxType }
func (tx *LegacyTx) chainID() *big.Int       { return nil }
func (tx *LegacyTx) input() []byte           { return tx.Input }
func (tx *LegacyTx) gas() uint64             { return tx.Gas }
func (tx *LegacyTx) gasPrice() *big.Int      { return tx.GasPrice }
func (tx *LegacyTx) gasTipCap() *big.Int     { return tx.GasPrice }
func (tx *LegacyTx) gasFeeCap() *big.Int     { return tx.GasPrice }
func (tx *LegacyTx) value() *big.Int         { return tx.Value }
func (tx *LegacyTx) nonce() uint64           { return tx.Nonce }
func (tx *LegacyTx) to() *Address            { return tx.To }
func (tx *LegacyTx) from() Address           { return tx.From }

func (tx *LegacyTx) hash() Hash { return tx.Hash }

func (tx *LegacyTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *LegacyTx) accessList() TxAccessList { return nil }

// set methods for transaction fields
func (tx *LegacyTx) setSignatureValues(v, r, s *big.Int) {
	tx.V, tx.R, tx.S = v, r, s
}

func (tx *LegacyTx) setFrom(addr Address) { tx.From = addr }

func (tx *LegacyTx) setGas(gas uint64) {
	tx.Gas = gas
}

func (tx *LegacyTx) setChainID(id *big.Int) {}

func (tx *LegacyTx) setGasPrice(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *LegacyTx) setGasFeeCap(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *LegacyTx) setGasTipCap(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *LegacyTx) setTransactionType(t TxType) {}

func (tx *LegacyTx) setValue(value *big.Int) {
	tx.Value = value
}

func (tx *LegacyTx) setInput(input []byte) {
	tx.Input = input
}

func (tx *LegacyTx) setTo(addeess *Address) {
	tx.To = addeess
}

func (tx *LegacyTx) setNonce(nonce uint64) {
	tx.Nonce = nonce
}

func (tx *LegacyTx) setAccessList(accessList TxAccessList) {}

func (tx *LegacyTx) setHash(h Hash) { tx.Hash = h }

// unmarshalRLPFrom unmarshals a Transaction in RLP format
// Be careful! This function does not de-serialize tx type, it assumes that t.Type is already set
// Hash calculation should also be done from the outside!
// Use UnmarshalRLP in most cases
func (tx *LegacyTx) unmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	num := 9

	elems, err := v.GetElems()
	if err != nil {
		return err
	}

	getElem := func() *fastrlp.Value {
		val := elems[0]
		elems = elems[1:]

		return val
	}

	if numElems := len(elems); numElems != num {
		return fmt.Errorf("legacy incorrect number of transaction elements, expected %d but found %d", num, numElems)
	}

	// nonce
	txNonce, err := getElem().GetUint64()
	if err != nil {
		return err
	}

	tx.setNonce(txNonce)

	// gasPrice
	txGasPrice := new(big.Int)
	if err = getElem().GetBigInt(txGasPrice); err != nil {
		return err
	}

	tx.setGasPrice(txGasPrice)

	// gas
	txGas, err := getElem().GetUint64()
	if err != nil {
		return err
	}

	tx.setGas(txGas)

	// to
	if vv, _ := getElem().Bytes(); len(vv) == 20 {
		// address
		addr := BytesToAddress(vv)
		tx.setTo(&addr)
	} else {
		// reset To
		tx.setTo(nil)
	}

	// value
	txValue := new(big.Int)
	if err = getElem().GetBigInt(txValue); err != nil {
		return err
	}

	tx.setValue(txValue)

	// input
	var txInput []byte

	txInput, err = getElem().GetBytes(txInput)
	if err != nil {
		return err
	}

	tx.setInput(txInput)

	// V
	txV := new(big.Int)
	if err = getElem().GetBigInt(txV); err != nil {
		return err
	}

	// R
	txR := new(big.Int)
	if err = getElem().GetBigInt(txR); err != nil {
		return err
	}

	// S
	txS := new(big.Int)
	if err = getElem().GetBigInt(txS); err != nil {
		return err
	}

	tx.setSignatureValues(txV, txR, txS)

	return nil
}

// MarshalRLPWith marshals the transaction to RLP with a specific fastrlp.Arena
// Be careful! This function does not serialize tx type as a first byte.
// Use MarshalRLP/MarshalRLPTo in most cases
func (tx *LegacyTx) marshalRLPWith(arena *fastrlp.Arena) *fastrlp.Value {
	vv := arena.NewArray()

	vv.Set(arena.NewUint(tx.nonce()))
	vv.Set(arena.NewBigInt(tx.gasPrice()))
	vv.Set(arena.NewUint(tx.gas()))

	// Address may be empty
	if tx.to() != nil {
		vv.Set(arena.NewCopyBytes(tx.to().Bytes()))
	} else {
		vv.Set(arena.NewNull())
	}

	vv.Set(arena.NewBigInt(tx.value()))
	vv.Set(arena.NewCopyBytes(tx.input()))

	// signature values
	v, r, s := tx.rawSignatureValues()
	vv.Set(arena.NewBigInt(v))
	vv.Set(arena.NewBigInt(r))
	vv.Set(arena.NewBigInt(s))

	return vv
}

func (tx *LegacyTx) copy() TxData { //nolint:dupl
	cpy := &LegacyTx{}

	cpy.setNonce(tx.nonce())

	if tx.gasPrice() != nil {
		gasPrice := new(big.Int)
		gasPrice.Set(tx.gasPrice())

		cpy.setGasPrice(gasPrice)
	}

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
