package types

import (
	"fmt"
	"math/big"

	"github.com/umbracle/fastrlp"
)

type StateTx struct {
	GasPrice *big.Int
	BaseTx   *BaseTx
}

func (tx *StateTx) transactionType() TxType { return StateTxType }
func (tx *StateTx) chainID() *big.Int       { return deriveChainID(tx.BaseTx.V) }
func (tx *StateTx) input() []byte           { return tx.BaseTx.input() }
func (tx *StateTx) gas() uint64             { return tx.BaseTx.gas() }
func (tx *StateTx) gasPrice() *big.Int      { return tx.GasPrice }
func (tx *StateTx) gasTipCap() *big.Int     { return tx.GasPrice }
func (tx *StateTx) gasFeeCap() *big.Int     { return tx.GasPrice }
func (tx *StateTx) value() *big.Int         { return tx.BaseTx.value() }
func (tx *StateTx) nonce() uint64           { return tx.BaseTx.nonce() }
func (tx *StateTx) to() *Address            { return tx.BaseTx.to() }
func (tx *StateTx) from() Address           { return tx.BaseTx.from() }
func (tx *StateTx) baseTx() *BaseTx         { return tx.BaseTx }

func (tx *StateTx) hash() Hash { return tx.BaseTx.hash() }

func (tx *StateTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.BaseTx.rawSignatureValues()
}

func (tx *StateTx) accessList() TxAccessList {
	return nil
}

// set methods for transaction fields
func (tx *StateTx) setSignatureValues(v, r, s *big.Int) {
	tx.BaseTx.setSignatureValues(v, r, s)
}

func (tx *StateTx) setFrom(addr Address) {
	tx.setFrom(addr)
}

func (tx *StateTx) setGas(gas uint64) {
	tx.setGas(gas)
}

func (tx *StateTx) setChainID(id *big.Int) {}

func (tx *StateTx) setGasPrice(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *StateTx) setGasFeeCap(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *StateTx) setGasTipCap(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *StateTx) setValue(value *big.Int) {
	tx.setValue(value)
}

func (tx *StateTx) setInput(input []byte) {
	tx.setInput(input)
}

func (tx *StateTx) setTo(addeess *Address) {
	tx.setTo(addeess)
}

func (tx *StateTx) setNonce(nonce uint64) {
	tx.setNonce(nonce)
}

func (tx *StateTx) setAccessList(accessList TxAccessList) {}

func (tx *StateTx) setHash(h Hash) { tx.BaseTx.setHash(h) }

func (tx *StateTx) setBaseTx(base *BaseTx) {
	tx.BaseTx = base
}

// unmarshalRLPFrom unmarshals a Transaction in RLP format
// Be careful! This function does not de-serialize tx type, it assumes that t.Type is already set
// Hash calculation should also be done from the outside!
// Use UnmarshalRLP in most cases
func (tx *StateTx) unmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	numOfElems := 10

	var (
		values rlpValues
		err    error
	)

	values, err = v.GetElems()
	if err != nil {
		return err
	}

	if numElems := len(values); numElems != numOfElems {
		return fmt.Errorf("incorrect number of transaction elements, expected %d but found %d", numOfElems, numElems)
	}

	// gasPrice
	txGasPrice := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txGasPrice); err != nil {
		return err
	}

	tx.setGasPrice(txGasPrice)

	baseTx := new(BaseTx)
	if err = baseTx.unmarshalRLPFrom(values); err != nil {
		return err
	}

	tx.setBaseTx(baseTx)

	tx.setFrom(ZeroAddress)

	// We need to set From field for state transaction,
	// because we are using unique, predefined address, for sending such transactions
	if vv, err := values.dequeueValue().Bytes(); err == nil && len(vv) == AddressLength {
		// address
		tx.setFrom(BytesToAddress(vv))
	}

	return nil
}

// MarshalRLPWith marshals the transaction to RLP with a specific fastrlp.Arena
// Be careful! This function does not serialize tx type as a first byte.
// Use MarshalRLP/MarshalRLPTo in most cases
func (tx *StateTx) marshalRLPWith(arena *fastrlp.Arena) *fastrlp.Value {
	vv := arena.NewArray()

	vv.Set(arena.NewBigInt(tx.gasPrice()))

	vv.Set(tx.baseTx().marshalRLPWith(arena))

	vv.Set(arena.NewCopyBytes(tx.from().Bytes()))

	return vv
}

func (tx *StateTx) copy() TxData { //nolint:dupl
	cpy := &StateTx{}

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
