package types

import (
	"fmt"
	"math/big"

	"github.com/umbracle/fastrlp"
)

type TxAccessList []AccessTuple

type AccessTuple struct {
	Address     Address
	StorageKeys []Hash
}

// StorageKeys returns the total number of storage keys in the access list.
func (al TxAccessList) StorageKeys() int {
	sum := 0
	for _, tuple := range al {
		sum += len(tuple.StorageKeys)
	}

	return sum
}

// Copy makes a deep copy of the access list.
func (al TxAccessList) Copy() TxAccessList {
	if al == nil {
		return nil
	}

	newAccessList := make(TxAccessList, len(al))

	for i, item := range al {
		var copiedAddress Address

		copy(copiedAddress[:], item.Address[:])
		newAccessList[i] = AccessTuple{
			Address:     copiedAddress,
			StorageKeys: append([]Hash{}, item.StorageKeys...),
		}
	}

	return newAccessList
}

func (al TxAccessList) UnmarshallRLPFrom(p *fastrlp.Parser, accessListVV []*fastrlp.Value) error {
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

		al[i].Address = BytesToAddress(addressBytes)

		// Read the storage keys
		storageKeysArrayVV := accessTupleElems[1]

		storageKeysElems, err := storageKeysArrayVV.GetElems()
		if err != nil {
			return err
		}

		al[i].StorageKeys = make([]Hash, len(storageKeysElems))

		for j, storageKeyVV := range storageKeysElems {
			storageKeyBytes, err := storageKeyVV.Bytes()
			if err != nil {
				return err
			}

			al[i].StorageKeys[j] = BytesToHash(storageKeyBytes)
		}
	}

	return nil
}

func (al TxAccessList) MarshallRLPWith(arena *fastrlp.Arena) *fastrlp.Value {
	accessListVV := arena.NewArray()

	for _, accessTuple := range al {
		accessTupleVV := arena.NewArray()
		accessTupleVV.Set(arena.NewCopyBytes(accessTuple.Address.Bytes()))

		storageKeysVV := arena.NewArray()
		for _, storageKey := range accessTuple.StorageKeys {
			storageKeysVV.Set(arena.NewCopyBytes(storageKey.Bytes()))
		}

		accessTupleVV.Set(storageKeysVV)
		accessListVV.Set(accessTupleVV)
	}

	return accessListVV
}

type AccessListTxn struct {
	GasPrice *big.Int
	BaseTx   *BaseTx

	ChainID    *big.Int
	AccessList TxAccessList
}

func (tx *AccessListTxn) transactionType() TxType { return AccessListTxType }
func (tx *AccessListTxn) chainID() *big.Int       { return tx.ChainID }
func (tx *AccessListTxn) input() []byte           { return tx.BaseTx.input() }
func (tx *AccessListTxn) gas() uint64             { return tx.BaseTx.gas() }
func (tx *AccessListTxn) gasPrice() *big.Int      { return tx.GasPrice }
func (tx *AccessListTxn) gasTipCap() *big.Int     { return tx.GasPrice }
func (tx *AccessListTxn) gasFeeCap() *big.Int     { return tx.GasPrice }
func (tx *AccessListTxn) value() *big.Int         { return tx.BaseTx.value() }
func (tx *AccessListTxn) nonce() uint64           { return tx.BaseTx.nonce() }
func (tx *AccessListTxn) to() *Address            { return tx.BaseTx.to() }
func (tx *AccessListTxn) from() Address           { return tx.BaseTx.from() }
func (tx *AccessListTxn) baseTx() *BaseTx         { return tx.BaseTx }

func (tx *AccessListTxn) hash() Hash { return tx.BaseTx.hash() }

func (tx *AccessListTxn) rawSignatureValues() (v, r, s *big.Int) {
	return tx.BaseTx.rawSignatureValues()
}

func (tx *AccessListTxn) accessList() TxAccessList {
	return tx.AccessList
}

// set methods for transaction fields
func (tx *AccessListTxn) setSignatureValues(v, r, s *big.Int) {
	tx.BaseTx.setSignatureValues(v, r, s)
}

func (tx *AccessListTxn) setFrom(addr Address) {
	tx.BaseTx.setFrom(addr)
}

func (tx *AccessListTxn) setGas(gas uint64) {
	tx.BaseTx.setGas(gas)
}

func (tx *AccessListTxn) setChainID(id *big.Int) {
	tx.ChainID = id
}

func (tx *AccessListTxn) setGasPrice(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *AccessListTxn) setGasFeeCap(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *AccessListTxn) setGasTipCap(gas *big.Int) {
	tx.GasPrice = gas
}

func (tx *AccessListTxn) setTransactionType(t TxType) {
	// no need to set a transaction type for access list type of transaction
}

func (tx *AccessListTxn) setValue(value *big.Int) {
	tx.BaseTx.setValue(value)
}

func (tx *AccessListTxn) setInput(input []byte) {
	tx.BaseTx.setInput(input)
}

func (tx *AccessListTxn) setTo(address *Address) {
	tx.setTo(address)
}

func (tx *AccessListTxn) setNonce(nonce uint64) {
	tx.BaseTx.setNonce(nonce)
}

func (tx *AccessListTxn) setAccessList(accessList TxAccessList) {
	tx.AccessList = accessList
}

func (tx *AccessListTxn) setHash(h Hash) {
	tx.setHash(h)
}

func (tx *AccessListTxn) setBaseTx(base *BaseTx) {
	tx.BaseTx = base
}

// unmarshalRLPFrom unmarshals a Transaction in RLP format
// Be careful! This function does not de-serialize tx type, it assumes that t.Type is already set
// Hash calculation should also be done from the outside!
// Use UnmarshalRLP in most cases
func (tx *AccessListTxn) unmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	numOfElems := 11

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

	txChainID := new(big.Int)

	if err = values.dequeueValue().GetBigInt(txChainID); err != nil {
		return err
	}

	tx.setChainID(txChainID)

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

	//accessList
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
func (tx *AccessListTxn) marshalRLPWith(arena *fastrlp.Arena) *fastrlp.Value {
	vv := arena.NewArray()

	vv.Set(arena.NewBigInt(tx.chainID()))
	vv.Set(arena.NewBigInt(tx.gasPrice()))

	vv.Set(tx.BaseTx.marshalRLPWith(arena))

	// Convert TxAccessList to RLP format and add it to the vv array.
	vv.Set(tx.accessList().MarshallRLPWith(arena))

	return vv
}

func (tx *AccessListTxn) copy() TxData {
	cpy := &AccessListTxn{}

	if tx.chainID() != nil {
		chainID := new(big.Int)
		chainID.Set(tx.chainID())

		cpy.setChainID(chainID)
	}

	if tx.gasPrice() != nil {
		gasPrice := new(big.Int)
		gasPrice.Set(tx.gasPrice())

		cpy.setGasPrice(gasPrice)
	}

	if tx.baseTx() != nil {
		cpy.setBaseTx(tx.baseTx().copy())
	}

	cpy.setAccessList(tx.accessList().Copy())

	return cpy
}
