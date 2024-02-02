package types

import (
	"fmt"
	"math/big"

	"github.com/umbracle/fastrlp"
)

type DynamicFeeTx struct {
	Nonce     uint64
	GasTipCap *big.Int
	GasFeeCap *big.Int
	Gas       uint64
	To        *Address
	Value     *big.Int
	Input     []byte
	V, R, S   *big.Int
	Hash      Hash
	From      Address

	ChainID    *big.Int
	AccessList TxAccessList
}

func (tx *DynamicFeeTx) transactionType() TxType { return DynamicFeeTxType }
func (tx *DynamicFeeTx) chainID() *big.Int       { return tx.ChainID }
func (tx *DynamicFeeTx) input() []byte           { return tx.Input }
func (tx *DynamicFeeTx) gas() uint64             { return tx.Gas }
func (tx *DynamicFeeTx) gasPrice() *big.Int      { return nil }
func (tx *DynamicFeeTx) gasTipCap() *big.Int     { return tx.GasTipCap }
func (tx *DynamicFeeTx) gasFeeCap() *big.Int     { return tx.GasFeeCap }
func (tx *DynamicFeeTx) value() *big.Int         { return tx.Value }
func (tx *DynamicFeeTx) nonce() uint64           { return tx.Nonce }
func (tx *DynamicFeeTx) to() *Address            { return tx.To }
func (tx *DynamicFeeTx) from() Address           { return tx.From }

func (tx *DynamicFeeTx) hash() Hash { return ZeroHash }

func (tx *DynamicFeeTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *DynamicFeeTx) accessList() TxAccessList { return tx.AccessList }

func (tx *DynamicFeeTx) setSignatureValues(v, r, s *big.Int) {
	tx.V, tx.R, tx.S = v, r, s
}

func (tx *DynamicFeeTx) setFrom(addr Address) { tx.From = addr }

func (tx *DynamicFeeTx) setGas(gas uint64) {
	tx.Gas = gas
}

func (tx *DynamicFeeTx) setChainID(id *big.Int) {
	tx.ChainID = id
}

func (tx *DynamicFeeTx) setGasPrice(gas *big.Int) {}

func (tx *DynamicFeeTx) setGasFeeCap(gas *big.Int) {
	tx.GasFeeCap = gas
}

func (tx *DynamicFeeTx) setGasTipCap(gas *big.Int) {
	tx.GasTipCap = gas
}

func (tx *DynamicFeeTx) setValue(value *big.Int) {
	tx.Value = value
}

func (tx *DynamicFeeTx) setInput(input []byte) {
	tx.Input = input
}

func (tx *DynamicFeeTx) setTo(address *Address) {
	tx.To = address
}

func (tx *DynamicFeeTx) setNonce(nonce uint64) {
	tx.Nonce = nonce
}

func (tx *DynamicFeeTx) setAccessList(accessList TxAccessList) {
	tx.AccessList = accessList
}

func (tx *DynamicFeeTx) setHash(h Hash) { tx.Hash = h }

// unmarshalRLPFrom unmarshals a Transaction in RLP format
// Be careful! This function does not de-serialize tx type, it assumes that t.Type is already set
// Hash calculation should also be done from the outside!
// Use UnmarshalRLP in most cases
func (tx *DynamicFeeTx) unmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	numOfElems := 12
	elems, err := v.GetElems()
	if err != nil {
		return err
	}

	getElem := func() *fastrlp.Value {
		val := elems[0]
		elems = elems[1:]

		return val
	}

	if numElems := len(elems); numElems != numOfElems {
		return fmt.Errorf("incorrect number of transaction elements, expected %d but found %d", numOfElems, numElems)
	}

	// Load Chain ID
	txChainID := new(big.Int)
	if err = getElem().GetBigInt(txChainID); err != nil {
		return err
	}

	tx.setChainID(txChainID)

	// nonce
	txNonce, err := getElem().GetUint64()
	if err != nil {
		return err
	}

	tx.setNonce(txNonce)

	// gasTipCap
	txGasTipCap := new(big.Int)
	if err = getElem().GetBigInt(txGasTipCap); err != nil {
		return err
	}

	tx.setGasTipCap(txGasTipCap)

	// gasFeeCap
	txGasFeeCap := new(big.Int)
	if err = getElem().GetBigInt(txGasFeeCap); err != nil {
		return err
	}

	tx.setGasFeeCap(txGasFeeCap)

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

	accessListVV, err := getElem().GetElems()
	if err != nil {
		return err
	}

	var txAccessList TxAccessList
	if len(accessListVV) != 0 {
		txAccessList = make(TxAccessList, len(accessListVV))
	}

	for i, accessTupleVV := range accessListVV {
		accessTupleElems, err := accessTupleVV.GetElems()
		if err != nil {
			return err
		}

		// Read the address
		addressVV := accessTupleElems[0]

		addressBytes, err := addressVV.Bytes()
		if err != nil {
			return err
		}

		txAccessList[i].Address = BytesToAddress(addressBytes)

		// Read the storage keys
		storageKeysArrayVV := accessTupleElems[1]

		storageKeysElems, err := storageKeysArrayVV.GetElems()
		if err != nil {
			return err
		}

		txAccessList[i].StorageKeys = make([]Hash, len(storageKeysElems))

		for j, storageKeyVV := range storageKeysElems {
			storageKeyBytes, err := storageKeyVV.Bytes()
			if err != nil {
				return err
			}

			txAccessList[i].StorageKeys[j] = BytesToHash(storageKeyBytes)
		}
	}

	tx.setAccessList(txAccessList)

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
func (tx *DynamicFeeTx) marshalRLPWith(arena *fastrlp.Arena) *fastrlp.Value {
	vv := arena.NewArray()

	vv.Set(arena.NewBigInt(tx.chainID()))
	vv.Set(arena.NewUint(tx.nonce()))
	// Add EIP-1559 related fields.
	// For non-dynamic-fee-tx gas price is used.
	vv.Set(arena.NewBigInt(tx.gasTipCap()))
	vv.Set(arena.NewBigInt(tx.gasFeeCap()))
	vv.Set(arena.NewUint(tx.gas()))

	// Address may be empty
	if tx.to() != nil {
		vv.Set(arena.NewCopyBytes(tx.to().Bytes()))
	} else {
		vv.Set(arena.NewNull())
	}

	vv.Set(arena.NewBigInt(tx.value()))
	vv.Set(arena.NewCopyBytes(tx.input()))

	// Convert TxAccessList to RLP format and add it to the vv array.
	accessListVV := arena.NewArray()

	for _, accessTuple := range tx.accessList() {
		accessTupleVV := arena.NewArray()
		accessTupleVV.Set(arena.NewCopyBytes(accessTuple.Address.Bytes()))

		storageKeysVV := arena.NewArray()
		for _, storageKey := range accessTuple.StorageKeys {
			storageKeysVV.Set(arena.NewCopyBytes(storageKey.Bytes()))
		}

		accessTupleVV.Set(storageKeysVV)
		accessListVV.Set(accessTupleVV)
	}

	vv.Set(accessListVV)

	// signature values
	v, r, s := tx.rawSignatureValues()
	vv.Set(arena.NewBigInt(v))
	vv.Set(arena.NewBigInt(r))
	vv.Set(arena.NewBigInt(s))

	return vv
}

func (tx *DynamicFeeTx) copy() TxData {
	cpy := &DynamicFeeTx{}

	if tx.chainID() != nil {
		chainID := new(big.Int)
		chainID.Set(tx.chainID())

		cpy.setChainID(chainID)
	}

	cpy.setNonce(tx.nonce())

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

	cpy.setAccessList(tx.accessList().Copy())

	cpy.setHash(tx.hash())

	return cpy
}
