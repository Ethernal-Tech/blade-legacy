package types

import (
	"fmt"
	"math/big"

	"github.com/umbracle/fastrlp"
)

type LegacyTx struct {
	GasPrice *big.Int
	BaseTx   *BaseTx
}

func (tx *LegacyTx) transactionType() TxType { return LegacyTxType }
func (tx *LegacyTx) chainID() *big.Int       { return deriveChainID(tx.baseTx().V) }
func (tx *LegacyTx) input() []byte           { return tx.baseTx().input() }
func (tx *LegacyTx) gas() uint64             { return tx.baseTx().gas() }
func (tx *LegacyTx) gasPrice() *big.Int      { return tx.GasPrice }
func (tx *LegacyTx) gasTipCap() *big.Int     { return tx.GasPrice }
func (tx *LegacyTx) gasFeeCap() *big.Int     { return tx.GasPrice }
func (tx *LegacyTx) value() *big.Int         { return tx.baseTx().value() }
func (tx *LegacyTx) nonce() uint64           { return tx.baseTx().nonce() }
func (tx *LegacyTx) to() *Address            { return tx.baseTx().to() }
func (tx *LegacyTx) from() Address           { return tx.baseTx().from() }
func (tx *LegacyTx) baseTx() *BaseTx         { return tx.BaseTx }

func (tx *LegacyTx) hash() Hash { return tx.BaseTx.hash() }

func (tx *LegacyTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.baseTx().rawSignatureValues()
}

func (tx *LegacyTx) accessList() TxAccessList { return nil }

// set methods for transaction fields
func (tx *LegacyTx) setSignatureValues(v, r, s *big.Int) {
	tx.baseTx().setSignatureValues(v, r, s)
}

func (tx *LegacyTx) setFrom(addr Address) { tx.baseTx().setFrom(addr) }

func (tx *LegacyTx) setGas(gas uint64) {
	tx.baseTx().setGas(gas)
}

func (tx *LegacyTx) setChainID(id *big.Int) {}

func (tx *LegacyTx) setGasPrice(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *LegacyTx) setGasFeeCap(gas *big.Int) {
}

func (tx *LegacyTx) setGasTipCap(gas *big.Int) {

}

func (tx *LegacyTx) setTransactionType(t TxType) {}

func (tx *LegacyTx) setValue(value *big.Int) {
	tx.baseTx().setValue(value)
}

func (tx *LegacyTx) setInput(input []byte) {
	tx.baseTx().setInput(input)
}

func (tx *LegacyTx) setTo(address *Address) {
	tx.baseTx().setTo(address)
}

func (tx *LegacyTx) setNonce(nonce uint64) {
	tx.baseTx().setNonce(nonce)
}

func (tx *LegacyTx) setAccessList(accessList TxAccessList) {}

func (tx *LegacyTx) setHash(h Hash) { tx.BaseTx.setHash(h) }

func (tx *LegacyTx) setBaseTx(base *BaseTx) {
	tx.BaseTx = base
}

// unmarshalRLPFrom unmarshals a Transaction in RLP format
// Be careful! This function does not de-serialize tx type, it assumes that t.Type is already set
// Hash calculation should also be done from the outside!
// Use UnmarshalRLP in most cases
func (tx *LegacyTx) unmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	num := 9

	var (
		values rlpValues
		err    error
	)

	values, err = v.GetElems()
	if err != nil {
		return err
	}

	if numElems := len(values); numElems != num {
		return fmt.Errorf("legacy incorrect number of transaction elements, expected %d but found %d", num, numElems)
	}

	// nonce
	txNonce, err := values.dequeueValue().GetUint64()
	if err != nil {
		return err
	}

	tx.setNonce(txNonce)

	// gasPrice
	txGasPrice := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txGasPrice); err != nil {
		return err
	}

	tx.setGasPrice(txGasPrice)

	// gas
	txGas, err := values.dequeueValue().GetUint64()
	if err != nil {
		return err
	}

	tx.setGas(txGas)

	// to
	if vv, _ := values.dequeueValue().Bytes(); len(vv) == 20 {
		// address
		addr := BytesToAddress(vv)
		tx.setTo(&addr)
	} else {
		// reset To
		tx.setTo(nil)
	}

	// value
	txValue := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txValue); err != nil {
		return err
	}

	tx.setValue(txValue)

	// input
	var txInput []byte

	txInput, err = values.dequeueValue().GetBytes(txInput)
	if err != nil {
		return err
	}

	tx.setInput(txInput)

	// V
	txV := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txV); err != nil {
		return err
	}

	// R
	txR := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txR); err != nil {
		return err
	}

	// S
	txS := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txS); err != nil {
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
	cpy := &LegacyTx{BaseTx: &BaseTx{}}

	if tx.gasPrice() != nil {
		gasPrice := new(big.Int)
		gasPrice.Set(tx.gasPrice())

		cpy.setGasPrice(gasPrice)
	}

	if tx.baseTx() != nil {
		cpy.setBaseTx(tx.baseTx().copy())
	}

	return cpy
}

// deriveChainID derives the chain id from the given v parameter
func deriveChainID(v *big.Int) *big.Int {
	if v != nil && v.BitLen() <= 64 {
		v := v.Uint64()
		if v == 27 || v == 28 {
			return new(big.Int)
		}

		return new(big.Int).SetUint64((v - 35) / 2)
	}

	v = new(big.Int).Sub(v, big.NewInt(35))

	return v.Div(v, big.NewInt(2))
}
