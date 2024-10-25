package state

import (
	"bytes"
	"fmt"
	"math/big"

	iradix "github.com/hashicorp/go-immutable-radix"
	"github.com/umbracle/fastrlp"

	"github.com/0xPolygon/polygon-edge/types"
)

// State represents an interface for interacting with a state that can be
// snapshotted, queried for data, and checked for existence of specific items.
type State interface {

	// NewSnapshot creates a new state snapshot based on the provided root hash.
	// This can be useful to capture a point-in-time view of the state.
	//
	// Parameters:
	// - rootHash: The hash representing the root state.
	//
	// Returns:
	// - Snapshot: The newly created snapshot of the state.
	// - error: An error if the snapshot could not be created.
	NewSnapshot(rootHash types.Hash) (Snapshot, error)

	// GetCode retrieves the bytecode associated with a specific code hash.
	//
	// Parameters:
	// - hash: The hash of the code.
	//
	// Returns:
	// - []byte: The bytecode corresponding to the hash.
	// - bool: A boolean indicating whether the code exists.
	GetCode(hash types.Hash) ([]byte, bool)

	// Get retrieves the value associated with a specific key (hash).
	//
	// Parameters:
	// - hash: The hash of the item being queried.
	//
	// Returns:
	// - []byte: The value stored at the given hash.
	// - bool: A boolean indicating whether the value exists.
	// - error: An error if there was a problem retrieving the value.
	Get(hash types.Hash) ([]byte, bool, error)

	// Has checks whether a specific item exists in the state by its hash.
	//
	// Parameters:
	// - hash: The hash of the item to check.
	//
	// Returns:
	// - bool: A boolean indicating whether the item exists.
	Has(hash types.Hash) bool
}

type Snapshot interface {
	readSnapshot

	Commit(objs []*Object) (Snapshot, []byte, error)
}

// DumpAccount represents an account in the state.
type DumpAccount struct {
	Balance  string                `json:"balance"`
	Nonce    uint64                `json:"nonce"`
	Root     []byte                `json:"root"`
	CodeHash []byte                `json:"codeHash"`
	Code     []byte                `json:"code,omitempty"`
	Storage  map[types.Hash]string `json:"storage,omitempty"`
	Address  types.Address         `json:"address,omitempty"` // Address only present in iterative (line-by-line) mode
	Key      []byte                `json:"key,omitempty"`     // If we don't have address, we can output the key
}

// Dump represents the full dump in a collected format, as one large map.
type Dump struct {
	Root     []byte                        `json:"root"`
	Accounts map[types.Address]DumpAccount `json:"accounts"`
}

// DumpConfig is a set of options to control what portions of the state will be
// iterated and collected.
type DumpInfo struct {
	SkipCode          bool
	SkipStorage       bool
	OnlyWithAddresses bool
	Start             []byte
	Max               int
}

// IteratorDump is an implementation for iterating over data.
type IteratorDump struct {
	Dump
	Next []byte `json:"next,omitempty"` // nil if no more accounts
}

// Account is the account reference in the ethereum state
type Account struct {
	Nonce    uint64
	Balance  *big.Int
	Root     types.Hash
	CodeHash []byte
}

func (a *Account) MarshalWith(ar *fastrlp.Arena) *fastrlp.Value {
	v := ar.NewArray()
	v.Set(ar.NewUint(a.Nonce))
	v.Set(ar.NewBigInt(a.Balance))
	v.Set(ar.NewBytes(a.Root.Bytes()))
	v.Set(ar.NewBytes(a.CodeHash))

	return v
}

var accountParserPool fastrlp.ParserPool

func (a *Account) UnmarshalRlp(b []byte) error {
	p := accountParserPool.Get()
	defer accountParserPool.Put(p)

	v, err := p.Parse(b)
	if err != nil {
		return err
	}

	elems, err := v.GetElems()

	if err != nil {
		return err
	}

	if len(elems) < 4 {
		return fmt.Errorf("incorrect number of elements to decode account, expected 4 but found %d", len(elems))
	}

	// nonce
	if a.Nonce, err = elems[0].GetUint64(); err != nil {
		return err
	}
	// balance
	if a.Balance == nil {
		a.Balance = new(big.Int)
	}

	if err = elems[1].GetBigInt(a.Balance); err != nil {
		return err
	}
	// root
	if err = elems[2].GetHash(a.Root[:]); err != nil {
		return err
	}
	// codeHash
	if a.CodeHash, err = elems[3].GetBytes(a.CodeHash[:0]); err != nil {
		return err
	}

	return nil
}

func (a *Account) String() string {
	return fmt.Sprintf("%d %s", a.Nonce, a.Balance.String())
}

func (a *Account) Copy() *Account {
	aa := new(Account)

	aa.Balance = big.NewInt(1).SetBytes(a.Balance.Bytes())
	aa.Nonce = a.Nonce
	aa.CodeHash = a.CodeHash
	aa.Root = a.Root

	return aa
}

// StateObject is the internal representation of the account
type StateObject struct {
	Account   *Account
	Code      []byte
	Suicide   bool
	Deleted   bool
	DirtyCode bool
	Txn       *iradix.Txn

	// withFakeStorage signals whether the state object
	// is using the override full state
	withFakeStorage bool
}

func (s *StateObject) Empty() bool {
	return s.Account.Nonce == 0 &&
		s.Account.Balance.Sign() == 0 &&
		bytes.Equal(s.Account.CodeHash, types.EmptyCodeHash.Bytes())
}

// Copy makes a copy of the state object
func (s *StateObject) Copy() *StateObject {
	ss := new(StateObject)

	// copy account
	ss.Account = s.Account.Copy()

	ss.Suicide = s.Suicide
	ss.Deleted = s.Deleted
	ss.DirtyCode = s.DirtyCode
	ss.Code = s.Code
	ss.withFakeStorage = s.withFakeStorage

	if s.Txn != nil {
		ss.Txn = s.Txn.CommitOnly().Txn()
	}

	return ss
}

// Object is the serialization of the radix object (can be merged to StateObject?).
type Object struct {
	Address  types.Address
	CodeHash types.Hash
	Balance  *big.Int
	Root     types.Hash
	Nonce    uint64
	Deleted  bool

	//nolint:godox
	// TODO: Move this to executor (to be fixed in EVM-527)
	DirtyCode bool
	Code      []byte

	Storage []*StorageObject
}

func (o *Object) Equals(other *Object) bool {
	// Compare Address, CodeHash, Root, Nonce, Deleted, and DirtyCode directly.
	if o.Address != other.Address ||
		o.CodeHash != other.CodeHash ||
		o.Root != other.Root ||
		o.Nonce != other.Nonce ||
		o.Deleted != other.Deleted ||
		o.DirtyCode != other.DirtyCode {
		return false
	}

	// Compare Balance.
	if o.Balance.Cmp(other.Balance) != 0 {
		return false
	}

	// Compare Code slices.
	if !bytes.Equal(o.Code, other.Code) {
		return false
	}

	// Compare Storage slices by length first.
	if len(o.Storage) != len(other.Storage) {
		return false
	}

	for i, storageObj := range o.Storage {
		if !(storageObj.Equals(other.Storage[i])) {
			return false
		}
	}

	return true
}

// StorageObject is an entry in the storage
type StorageObject struct {
	Deleted bool
	Key     []byte
	Val     []byte
}

func (s *StorageObject) Equals(other *StorageObject) bool {
	// Compare Deleted field directly
	if s.Deleted != other.Deleted {
		return false
	}

	// Compare Key and Val byte slices using bytes.Equal
	if !bytes.Equal(s.Key, other.Key) || !bytes.Equal(s.Val, other.Val) {
		return false
	}

	return true
}
