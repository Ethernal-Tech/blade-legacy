package keystore

import (
	"crypto/ecdsa"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/event"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
)

var (
	ErrLocked  = accounts.NewAuthNeededError("password or unlock")
	ErrNoMatch = errors.New("no key for given address or file")
	ErrDecrypt = errors.New("could not decrypt key with given password")

	// ErrAccountAlreadyExists is returned if an account attempted to import is
	// already present in the keystore.
	ErrAccountAlreadyExists = errors.New("account already exists")

	DefaultStorage, _ = filepath.Abs(filepath.Join("data-storage")) //nolint:gocritic
)

var KeyStoreType = reflect.TypeOf(&KeyStore{})

// KeyStore manages a key storage directory on disk.
type KeyStore struct {
	keyEncryption keyStore                    // Storage backend, might be cleartext or encrypted
	cache         *accountCache               // In-memory account cache over the filesystem storage
	unlocked      map[types.Address]*unlocked // Currently unlocked account (decrypted private keys)

	wallets      []accounts.Wallet // Wrapper around keys
	eventHandler *event.EventHandler

	manager accounts.BackendManager

	mu sync.RWMutex
}

type unlocked struct {
	*Key
	abort chan struct{}
}

func NewKeyStore(keyDir string, scryptN, scryptP int, logger hclog.Logger) *KeyStore {
	ks := &KeyStore{keyEncryption: &keyStorePassphrase{scryptN, scryptP}}

	ks.init(keyDir, logger)

	return ks
}

func (ks *KeyStore) init(keyDir string, logger hclog.Logger) {
	ks.unlocked = make(map[types.Address]*unlocked)
	ks.cache = newAccountCache(keyDir, logger)

	accs := ks.cache.accounts()
	ks.wallets = make([]accounts.Wallet, len(accs))

	for i := 0; i < len(accs); i++ {
		ks.wallets[i] = &keyStoreWallet{account: accs[i], keyStore: ks}
	}
}

func (ks *KeyStore) Wallets() []accounts.Wallet {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	cpy := make([]accounts.Wallet, len(ks.wallets))

	copy(cpy, ks.wallets)

	return cpy
}

// zeroKey zeroes a private key in memory.
func zeroKey(k *ecdsa.PrivateKey) {
	b := k.D.Bits()
	clear(b)
}

func (ks *KeyStore) SetEventHandler(eventHandler *event.EventHandler) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.eventHandler = eventHandler
}

func (ks *KeyStore) HasAddress(addr types.Address) bool {
	return ks.cache.hasAddress(addr)
}

func (ks *KeyStore) Accounts() []accounts.Account {
	return ks.cache.accounts()
}

func (ks *KeyStore) Delete(a accounts.Account, passphrase string) error {
	// Decrypting the key isn't really necessary, but we do
	// it anyway to check the password and zero out the key
	// immediately afterwards.
	a, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return err
	}

	if key != nil {
		zeroKey(key.PrivateKey)
	}

	if err := ks.cache.delete(a); err != nil {
		return err
	}

	for i, wallet := range ks.wallets {
		if wallet.Accounts()[0].Address == a.Address {
			ks.wallets = append(ks.wallets[:i], ks.wallets[i+1:]...)

			break
		}
	}

	event := accounts.WalletEvent{Wallet: &keyStoreWallet{account: a, keyStore: ks}, Kind: accounts.WalletDropped}

	ks.eventHandler.Publish(accounts.WalletEventKey, event)

	return nil
}

func (ks *KeyStore) SignHash(a accounts.Account, hash []byte) ([]byte, error) {
	// Look up the key to sign with and abort if it cannot be found
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	unlockedKey, found := ks.unlocked[a.Address]
	if !found {
		return nil, ErrLocked
	}

	// Sign the hash using plain ECDSA operations
	return crypto.Sign(unlockedKey.PrivateKey, hash)
}

func (ks *KeyStore) SignTx(a accounts.Account, tx *types.Transaction) (*types.Transaction, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	unlockedKey, ok := ks.unlocked[a.Address]
	if !ok {
		return nil, ErrLocked
	}

	signer := ks.manager.GetSigner()

	return signer.SignTx(tx, unlockedKey.PrivateKey)
}

func (ks *KeyStore) SignHashWithPassphrase(a accounts.Account,
	passphrase string, hash []byte) (signature []byte, err error) {
	_, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return nil, err
	}

	defer zeroKey(key.PrivateKey)

	return crypto.Sign(key.PrivateKey, hash)
}

func (ks *KeyStore) SignTxWithPassphrase(a accounts.Account, passphrase string,
	tx *types.Transaction) (*types.Transaction, error) {
	_, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return nil, err
	}

	defer zeroKey(key.PrivateKey)

	signer := ks.manager.GetSigner()

	return signer.SignTx(tx, key.PrivateKey)
}

// Unlock unlocks the given account indefinitely.
func (ks *KeyStore) Unlock(a accounts.Account, passphrase string) error {
	return ks.TimedUnlock(a, passphrase, 0)
}

// Lock removes the private key with the given address from memory.
func (ks *KeyStore) Lock(addr types.Address) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if unl, found := ks.unlocked[addr]; found {
		ks.expire(addr, unl, time.Duration(0)*time.Nanosecond)
	}

	return nil
}

func (ks *KeyStore) TimedUnlock(a accounts.Account, passphrase string, timeout time.Duration) error {
	a, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return err
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	u, ok := ks.unlocked[a.Address]
	if ok {
		if u.abort == nil {
			zeroKey(key.PrivateKey)

			return nil
		}

		close(u.abort)
	}

	if timeout > 0 {
		u = &unlocked{Key: key, abort: make(chan struct{})}

		go ks.expire(a.Address, u, timeout)
	} else {
		u = &unlocked{Key: key}
	}

	ks.unlocked[a.Address] = u

	return nil
}

func (ks *KeyStore) expire(addr types.Address, u *unlocked, timeout time.Duration) {
	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case <-u.abort:
		// just quit
	case <-t.C:
		ks.mu.Lock()
		// only drop if it's still the same key instance that dropLater
		// was launched with. we can check that using pointer equality
		// because the map stores a new pointer every time the key is
		// unlocked.
		if ks.unlocked[addr] == u {
			zeroKey(u.PrivateKey)
			delete(ks.unlocked, addr)
		}

		ks.mu.Unlock()
	}
}

func (ks *KeyStore) getDecryptedKey(a accounts.Account, auth string) (accounts.Account, *Key, error) {
	a, encryptedKeyJSONV3, err := ks.cache.find(a)
	if err != nil {
		return a, nil, err
	}

	key, err := ks.keyEncryption.KeyDecryption(encryptedKeyJSONV3, auth)

	return a, key, err
}

func (ks *KeyStore) NewAccount(passphrase string) (accounts.Account, error) {
	encryptedKey, account, err := storeNewKey(ks.keyEncryption, passphrase)
	if err != nil {
		return accounts.Account{}, err
	}

	if err := ks.cache.add(account, encryptedKey); err != nil {
		return accounts.Account{}, err
	}

	ks.wallets = append(ks.wallets, &keyStoreWallet{account: account, keyStore: ks})

	event := accounts.WalletEvent{Wallet: &keyStoreWallet{account: account, keyStore: ks}, Kind: accounts.WalletArrived}

	ks.eventHandler.Publish(accounts.WalletEventKey, event)

	return account, nil
}

func (ks *KeyStore) ImportECDSA(priv *ecdsa.PrivateKey, passphrase string) (accounts.Account, error) {
	key := newKeyFromECDSA(priv)
	if ks.cache.hasAddress(key.Address) {
		return accounts.Account{
			Address: key.Address,
		}, ErrAccountAlreadyExists
	}

	return ks.importKey(key, passphrase)
}

func (ks *KeyStore) importKey(key *Key, passphrase string) (accounts.Account, error) {
	a := accounts.Account{Address: key.Address}

	encryptedKeyJSONV3, err := ks.keyEncryption.KeyEncryption(key, passphrase)
	if err != nil {
		return accounts.Account{}, err
	}

	if err := ks.cache.add(a, encryptedKeyJSONV3); err != nil {
		return accounts.Account{}, err
	}

	ks.wallets = append(ks.wallets, &keyStoreWallet{account: a, keyStore: ks})

	event := accounts.WalletEvent{Wallet: &keyStoreWallet{account: a, keyStore: ks}, Kind: accounts.WalletArrived}

	ks.eventHandler.Publish(accounts.WalletEventKey, event)

	return a, nil
}

func (ks *KeyStore) Update(a accounts.Account, passphrase, newPassphrase string) error {
	a, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return err
	}

	encryptedKey, err := ks.keyEncryption.KeyEncryption(key, newPassphrase)
	if err != nil {
		return err
	}

	return ks.cache.update(a, encryptedKey)
}

func (ks *KeyStore) SetManager(manager accounts.BackendManager) {
	ks.manager = manager
}
