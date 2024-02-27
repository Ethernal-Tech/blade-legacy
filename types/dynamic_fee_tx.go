package types

import (
	"fmt"
	"math/big"

	"github.com/umbracle/fastrlp"
)

type DynamicFeeTx struct {
	GasTipCap *big.Int
	GasFeeCap *big.Int
	BaseTx    *BaseTx

	ChainID    *big.Int
	AccessList TxAccessList
}

func (tx *DynamicFeeTx) transactionType() TxType { return DynamicFeeTxType }
func (tx *DynamicFeeTx) chainID() *big.Int       { return tx.ChainID }
func (tx *DynamicFeeTx) input() []byte           { return tx.BaseTx.input() }
func (tx *DynamicFeeTx) gas() uint64             { return tx.BaseTx.gas() }
func (tx *DynamicFeeTx) gasPrice() *big.Int      { return nil }
func (tx *DynamicFeeTx) gasTipCap() *big.Int     { return tx.GasTipCap }
func (tx *DynamicFeeTx) gasFeeCap() *big.Int     { return tx.GasFeeCap }
func (tx *DynamicFeeTx) value() *big.Int         { return tx.BaseTx.value() }
func (tx *DynamicFeeTx) nonce() uint64           { return tx.BaseTx.nonce() }
func (tx *DynamicFeeTx) to() *Address            { return tx.BaseTx.to() }
func (tx *DynamicFeeTx) from() Address           { return tx.BaseTx.from() }
func (tx *DynamicFeeTx) baseTx() *BaseTx         { return tx.BaseTx }

func (tx *DynamicFeeTx) hash() Hash { return tx.BaseTx.hash() }

func (tx *DynamicFeeTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.BaseTx.rawSignatureValues()
}

func (tx *DynamicFeeTx) accessList() TxAccessList { return tx.AccessList }

func (tx *DynamicFeeTx) setSignatureValues(v, r, s *big.Int) {
	tx.BaseTx.setSignatureValues(v, r, s)
}

func (tx *DynamicFeeTx) setFrom(addr Address) { tx.BaseTx.setFrom(addr) }

func (tx *DynamicFeeTx) setGas(gas uint64) {
	tx.setGas(gas)
}

func (tx *DynamicFeeTx) setChainID(id *big.Int) {
	tx.ChainID = id
}

func (tx *DynamicFeeTx) setGasPrice(gas *big.Int) {
	tx.GasTipCap = gas
}

func (tx *DynamicFeeTx) setGasFeeCap(gas *big.Int) {
	tx.GasFeeCap = gas
}

func (tx *DynamicFeeTx) setGasTipCap(gas *big.Int) {
	tx.GasTipCap = gas
}

func (tx *DynamicFeeTx) setValue(value *big.Int) {
	tx.BaseTx.setValue(value)
}

func (tx *DynamicFeeTx) setInput(input []byte) {
	tx.BaseTx.setInput(input)
}

func (tx *DynamicFeeTx) setTo(address *Address) {
	tx.BaseTx.setTo(address)
}

func (tx *DynamicFeeTx) setNonce(nonce uint64) {
	tx.BaseTx.setNonce(nonce)
}

func (tx *DynamicFeeTx) setAccessList(accessList TxAccessList) {
	tx.AccessList = accessList
}

func (tx *DynamicFeeTx) setHash(h Hash) { tx.BaseTx.setHash(h) }

func (tx *DynamicFeeTx) setBaseTx(base *BaseTx) {
	tx.BaseTx = base
}

// unmarshalRLPFrom unmarshals a Transaction in RLP format
// Be careful! This function does not de-serialize tx type, it assumes that t.Type is already set
// Hash calculation should also be done from the outside!
// Use UnmarshalRLP in most cases
func (tx *DynamicFeeTx) unmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	numOfElems := 12

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

	// Load Chain ID
	txChainID := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txChainID); err != nil {
		return err
	}

	tx.setChainID(txChainID)

	// gasTipCap
	txGasTipCap := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txGasTipCap); err != nil {
		return err
	}

	tx.setGasTipCap(txGasTipCap)

	// gasFeeCap
	txGasFeeCap := new(big.Int)
	if err = values.dequeueValue().GetBigInt(txGasFeeCap); err != nil {
		return err
	}

	tx.setGasFeeCap(txGasFeeCap)

	baseTx := new(BaseTx)
	if err = baseTx.unmarshalRLPFrom(values); err != nil {
		return err
	}

	tx.setBaseTx(baseTx)

	accessListVV, err := values.dequeueValue().GetElems()
	if err != nil {
		return err
	}

	var txAccessList TxAccessList
	if len(accessListVV) != 0 {
		txAccessList = make(TxAccessList, len(accessListVV))
	}

	if err = txAccessList.UnmarshallRLPFrom(p, accessListVV); err != nil {
		return err
	}

	tx.setAccessList(txAccessList)

	return nil
}

// MarshalRLPWith marshals the transaction to RLP with a specific fastrlp.Arena
// Be careful! This function does not serialize tx type as a first byte.
// Use MarshalRLP/MarshalRLPTo in most cases
func (tx *DynamicFeeTx) marshalRLPWith(arena *fastrlp.Arena) *fastrlp.Value {
	vv := arena.NewArray()

	vv.Set(arena.NewBigInt(tx.chainID()))
	// Add EIP-1559 related fields.
	// For non-dynamic-fee-tx gas price is used.
	vv.Set(arena.NewBigInt(tx.gasTipCap()))
	vv.Set(arena.NewBigInt(tx.gasFeeCap()))

	vv.Set(tx.BaseTx.marshalRLPWith(arena))

	// Convert TxAccessList to RLP format and add it to the vv array.
	vv.Set(tx.accessList().MarshallRLPWith(arena))

	return vv
}

func (tx *DynamicFeeTx) copy() TxData {
	cpy := &DynamicFeeTx{}

	if tx.chainID() != nil {
		chainID := new(big.Int)
		chainID.Set(tx.chainID())

		cpy.setChainID(chainID)
	}

	if tx.gasTipCap() != nil {
		gasTipCap := new(big.Int)
		gasTipCap.Set(tx.gasTipCap())

		cpy.setGasTipCap(gasTipCap)
	}

	if tx.gasFeeCap() != nil {
		gasFeeCap := new(big.Int)
		gasFeeCap.Set(tx.gasFeeCap())

		cpy.setGasFeeCap(gasFeeCap)
	}

	if tx.baseTx() != nil {
		cpy.setBaseTx(tx.baseTx().copy())
	}

	cpy.setAccessList(tx.accessList().Copy())

	return cpy
}
