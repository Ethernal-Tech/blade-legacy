package keystore

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

	DefaultStorage, _ = filepath.Abs(filepath.Join("data-storage"))
)

var KeyStoreType = reflect.TypeOf(&KeyStore{})

// Maximum time between wallet refreshes (if filesystem notifications don't work).
const walletRefreshCycle = 3 * time.Second

// KeyStore manages a key storage directory on disk.
type KeyStore struct {
	storage  keyStore                    // Storage backend, might be cleartext or encrypted
	cache    *accountCache               // In-memory account cache over the filesystem storage
	changes  chan struct{}               // Channel receiving change notifications from the cache
	unlocked map[types.Address]*unlocked // Currently unlocked account (decrypted private keys)

	wallets     []accounts.Wallet       // Wallet wrappers around the individual key files
	updateFeed  event.Feed              // Event feed to notify wallet additions/removals
	updateScope event.SubscriptionScope // Subscription scope tracking current live listeners
	updating    bool                    // Whether the event notification loop is running

	mu       sync.RWMutex
	importMu sync.Mutex // Import Mutex locks the import to prevent two insertions from racing
}

type unlocked struct {
	*Key
	abort chan struct{}
}

func NewKeyStore(keyDir string, scryptN, scryptP int, logger hclog.Logger) *KeyStore {
	var ks *KeyStore
	if keyDir == "" {
		ks = &KeyStore{storage: &keyStorePassphrase{DefaultStorage, scryptN, scryptP, false}}
	} else {
		ks = &KeyStore{storage: &keyStorePassphrase{keyDir, scryptN, scryptP, false}}
	}

	ks.init(keyDir, hclog.NewNullLogger()) //TO DO LOGGER

	return ks
}

func (ks *KeyStore) init(keyDir string, logger hclog.Logger) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.unlocked = make(map[types.Address]*unlocked)
	ks.cache, ks.changes = newAccountCache(keyDir, logger)

	runtime.SetFinalizer(ks, func(m *KeyStore) {
		m.cache.close()
	})

	accs := ks.cache.accounts()
	ks.wallets = make([]accounts.Wallet, len(accs))

	for i := 0; i < len(accs); i++ {
		ks.wallets[i] = &keyStoreWallet{account: accs[i], keyStore: ks}
	}
}

func (ks *KeyStore) Wallets() []accounts.Wallet {
	// Make sure the list of wallets is in sync with the account cache
	ks.refreshWallets()

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

func (ks *KeyStore) refreshWallets() {
	ks.mu.Lock()
	accs := ks.cache.accounts()

	var ( //nolint:prealloc
		wallets = make([]accounts.Wallet, 0, len(accs))
		events  []accounts.WalletEvent
	)

	for _, account := range accs {
		for len(ks.wallets) > 0 && ks.wallets[0].URL().Cmp(account.URL) < 0 {
			events = append(events, accounts.WalletEvent{Wallet: ks.wallets[0], Kind: accounts.WalletDropped})
			ks.wallets = ks.wallets[1:]
		}

		if len(ks.wallets) == 0 || ks.wallets[0].URL().Cmp(account.URL) > 0 {
			wallet := &keyStoreWallet{account: account, keyStore: ks}

			events = append(events, accounts.WalletEvent{Wallet: wallet, Kind: accounts.WalletArrived})
			wallets = append(wallets, wallet)

			continue
		}

		if ks.wallets[0].Accounts()[0] == account {
			wallets = append(wallets, ks.wallets[0])
			ks.wallets = ks.wallets[1:]
		}
	}

	for _, wallet := range ks.wallets {
		events = append(events, accounts.WalletEvent{Wallet: wallet, Kind: accounts.WalletDropped})
	}

	ks.wallets = wallets
	ks.mu.Unlock()

	for _, event := range events {
		ks.updateFeed.Send(event)
		_ = event
	}
}

func (ks *KeyStore) Subscribe(sink chan<- accounts.WalletEvent) event.Subscription {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	sub := ks.updateScope.Track(ks.updateFeed.Subscribe(sink))

	if !ks.updating {
		ks.updating = true
		go ks.updater()
	}

	return sub
}

func (ks *KeyStore) updater() {
	for {
		select {
		case <-ks.changes:
		case <-time.After(walletRefreshCycle):
		}

		ks.refreshWallets()

		ks.mu.Lock()
		if ks.updateScope.Count() == 0 {
			ks.updating = false

			ks.mu.Unlock()

			return
		}
		ks.mu.Unlock()
	}
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
	if key != nil {
		zeroKey(key.PrivateKey)
	}

	if err != nil {
		return err
	}
	// The order is crucial here. The key is dropped from the
	// cache after the file is gone so that a reload happening in
	// between won't insert it into the cache again.
	err = os.Remove(a.URL.Path)
	if err == nil {
		ks.cache.delete(a)
		ks.refreshWallets()
	}

	return err
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

func (ks *KeyStore) SignTx(a accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	unlockedKey, ok := ks.unlocked[a.Address]
	if !ok {
		return nil, ErrLocked
	}

	signer := crypto.LatestSignerForChainID(chainID.Uint64())

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
	tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	_, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return nil, err
	}

	defer zeroKey(key.PrivateKey)

	signer := crypto.LatestSignerForChainID(chainID.Uint64())

	return signer.SignTx(tx, key.PrivateKey)
}

// Unlock unlocks the given account indefinitely.
func (ks *KeyStore) Unlock(a accounts.Account, passphrase string) error {
	return ks.TimedUnlock(a, passphrase, 0)
}

// Lock removes the private key with the given address from memory.
func (ks *KeyStore) Lock(addr types.Address) error {
	ks.mu.Lock()

	if unl, found := ks.unlocked[addr]; found {
		ks.mu.Unlock()
		ks.expire(addr, unl, time.Duration(0)*time.Nanosecond)
	} else {
		ks.mu.Unlock()
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

func (ks *KeyStore) Find(a accounts.Account) (accounts.Account, error) {
	ks.cache.maybeReload()
	ks.cache.mu.Lock()
	a, err := ks.cache.find(a)
	ks.cache.mu.Unlock()

	return a, err
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
	a, err := ks.Find(a)
	if err != nil {
		return a, nil, err
	}

	key, err := ks.storage.GetKey(a.Address, a.URL.Path, auth)

	return a, key, err
}

func (ks *KeyStore) NewAccount(passphrase string) (accounts.Account, error) {
	_, account, err := storeNewKey(ks.storage, crand.Reader, passphrase)

	if err != nil {
		return accounts.Account{}, err
	}

	ks.cache.add(account)
	ks.refreshWallets()

	return account, nil
}

func (ks *KeyStore) Export(a accounts.Account, passphrase, newPassphrase string) (keyJSON []byte, err error) {
	_, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return nil, err
	}

	var N, P int

	if store, ok := ks.storage.(*keyStorePassphrase); ok {
		N, P = store.scryptN, store.scryptP
	} else {
		N, P = StandardScryptN, StandardScryptP
	}

	return EncryptKey(key, newPassphrase, N, P)
}

func (ks *KeyStore) Import(keyJSON []byte, passphrase, newPassphrase string) (accounts.Account, error) {
	key, err := DecryptKey(keyJSON, passphrase)

	if key != nil && key.PrivateKey != nil {
		defer zeroKey(key.PrivateKey)
	}

	if err != nil {
		return accounts.Account{}, err
	}

	ks.importMu.Lock()
	defer ks.importMu.Unlock()

	if ks.cache.hasAddress(key.Address) {
		return accounts.Account{
			Address: key.Address,
		}, ErrAccountAlreadyExists
	}

	return ks.importKey(key, newPassphrase)
}

func (ks *KeyStore) ImportECDSA(priv *ecdsa.PrivateKey, passphrase string) (accounts.Account, error) {
	ks.importMu.Lock()
	defer ks.importMu.Unlock()

	key := newKeyFromECDSA(priv)
	if ks.cache.hasAddress(key.Address) {
		return accounts.Account{
			Address: key.Address,
		}, ErrAccountAlreadyExists
	}

	return ks.importKey(key, passphrase)
}

func (ks *KeyStore) importKey(key *Key, passphrase string) (accounts.Account, error) {
	a := accounts.Account{Address: key.Address,
		URL: accounts.URL{Scheme: KeyStoreScheme, Path: ks.storage.JoinPath(keyFileName(key.Address))}}

	if err := ks.storage.StoreKey(a.URL.Path, key, passphrase); err != nil {
		return accounts.Account{}, err
	}

	ks.cache.add(a)
	ks.refreshWallets()

	return a, nil
}

func (ks *KeyStore) Update(a accounts.Account, passphrase, newPassphrase string) error {
	a, key, err := ks.getDecryptedKey(a, passphrase)
	if err != nil {
		return err
	}

	return ks.storage.StoreKey(a.URL.Path, key, newPassphrase)
}

func (ks *KeyStore) ImportPreSaleKey(keyJSON []byte, passphrase string) (accounts.Account, error) {
	a, _, err := importPreSaleKey(ks.storage, keyJSON, passphrase)

	if err != nil {
		return a, err
	}

	ks.cache.add(a)
	ks.refreshWallets()

	return a, nil
}

func (ks *KeyStore) isUpdating() bool {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	return ks.updating
}
