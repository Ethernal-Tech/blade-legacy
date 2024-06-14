package accounts

import (
	"fmt"
	"reflect"

	"github.com/0xPolygon/polygon-edge/accounts/event"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"golang.org/x/crypto/sha3"
)

type Account struct {
	Address types.Address `json:"address"`
}

// Wallet represents a software or hardware wallet that might contain one or more
// accounts (derived from the same seed).
type Wallet interface {
	// Status returns a textual status to aid the user in the current state of the
	// wallet
	Status() (string, error)

	// Open initializes access to a wallet instance.
	Open(passphrase string) error

	// Close releases any resources held by an open wallet instance.
	Close() error

	// Accounts retrieves the list of signing accounts the wallet is currently aware
	// of
	Accounts() []Account

	// Contains returns whether an account is part of this particular wallet or not.
	Contains(account Account) bool

	// SignData requests the wallet to sign the hash of the given data
	SignData(account Account, mimeType string, data []byte) ([]byte, error)

	// SignDataWithPassphrase is identical to SignData, but also takes a password
	SignDataWithPassphrase(account Account, passphrase, mimeType string, data []byte) ([]byte, error)

	// SignText requests the wallet to sign the hash of a given piece of data, prefixed
	// by the Ethereum prefix scheme
	// This method should return the signature in 'canonical' format, with v 0 or 1.
	SignText(account Account, text []byte) ([]byte, error)

	// SignTextWithPassphrase is identical to Signtext, but also takes a password
	SignTextWithPassphrase(account Account, passphrase string, hash []byte) ([]byte, error)

	// SignTx requests the wallet to sign the given transaction.
	SignTx(account Account, tx *types.Transaction) (*types.Transaction, error)

	// SignTxWithPassphrase is identical to SignTx, but also takes a password
	SignTxWithPassphrase(account Account, passphrase string,
		tx *types.Transaction) (*types.Transaction, error)
}

func TextHash(data []byte) []byte {
	hash, _ := TextAndHash(data)

	return hash
}

func TextAndHash(data []byte) ([]byte, string) {
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(data), data)
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(msg))

	return hasher.Sum(nil), msg
}

type WalletEventType int

const (
	// WalletArrived is fired when a new wallet is detected either via USB or via
	// a filesystem event in the keystore.
	WalletArrived WalletEventType = iota

	// WalletOpened is fired when a wallet is successfully opened with the purpose
	// of starting any background processes such as automatic key derivation.
	WalletOpened

	// WalletDropped
	WalletDropped
)

// WalletEvent is an event fired by an account backend when a wallet arrival or
// departure is detected.
type WalletEvent struct {
	Wallet Wallet          // Wallet instance arrived or departed
	Kind   WalletEventType // Event type that happened in the system
}

func (WalletEvent) Type() event.EventType {
	return event.WalletEventType
}

type Backend interface {
	// Wallets retrieves the list of wallets the backend is currently aware of
	Wallets() []Wallet

	// SetEventHandler set eventHandler on backend to push events
	SetEventHandler(eventHandler *event.EventHandler)

	// SetManager sets backend manager
	SetManager(manager BackendManager)
}

type BackendManager interface {
	// Checks for active forks at current block number and return signer
	GetSigner() crypto.TxSigner

	// Close stop updater in manager
	Close() error

	// Adds backend to list of backends
	AddBackend(backend Backend)

	// Return specific type of backend
	Backends(kind reflect.Type) []Backend

	// Return list of all wallets
	Wallets() []Wallet

	// Return all accounts
	Accounts() []types.Address

	// Search wallet with specific account
	Find(account Account) (Wallet, error)
}
