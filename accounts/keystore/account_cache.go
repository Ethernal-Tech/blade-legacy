package keystore

import (
	"errors"
	"path"
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
)

// KeyStoreScheme is the protocol scheme prefixing account and wallet URLs.
const KeyStoreScheme = "keystore"
const minReloadInterval = 2 * time.Second

// accountCache is a live index of all accounts in the keystore.
type accountCache struct {
	logger   hclog.Logger
	keydir   string
	mu       sync.Mutex
	allMap   map[types.Address]encryptedKeyJSONV3
	throttle *time.Timer
	notify   chan struct{}
	fileC    *fileCache
}

func newAccountCache(keyDir string, logger hclog.Logger) (*accountCache, chan struct{}) {
	ac := &accountCache{
		logger: logger,
		keydir: keyDir,
		notify: make(chan struct{}, 1),
		allMap: make(map[types.Address]encryptedKeyJSONV3),
	}

	keyPath := path.Join(keyDir, "keys.txt")

	ac.fileC, _ = NewFileCache(keyPath)

	ac.scanAccounts() //nolint:errcheck

	return ac, ac.notify
}

func (ac *accountCache) accounts() []accounts.Account {
	err := ac.scanAccounts()
	if err != nil {
		ac.logger.Debug("can't scan account's", "err", err)

		return []accounts.Account{}
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	cpy := make([]accounts.Account, len(ac.allMap))
	i := 0

	for addr := range ac.allMap {
		cpy[i] = accounts.Account{Address: addr}
		i++
	}

	return cpy
}

func (ac *accountCache) hasAddress(addr types.Address) bool {
	err := ac.scanAccounts()
	if err != nil {
		ac.logger.Debug("can't scan account's", "err", err)
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	_, ok := ac.allMap[addr]

	return ok
}

func (ac *accountCache) add(newAccount accounts.Account, key encryptedKeyJSONV3) error {
	err := ac.scanAccounts()
	if err != nil {
		return err
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, ok := ac.allMap[newAccount.Address]; ok {
		return errors.New("account already exists")
	}

	ac.allMap[newAccount.Address] = key

	err = ac.fileC.saveData(ac.allMap)
	if err != nil {
		return err
	}

	return nil
}

func (ac *accountCache) update(account accounts.Account, key encryptedKeyJSONV3) error {
	if err := ac.scanAccounts(); err != nil {
		return err
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, ok := ac.allMap[account.Address]; !ok {
		return errors.New("this account doesn't exists")
	} else {
		ac.allMap[account.Address] = key
	}

	if err := ac.fileC.saveData(ac.allMap); err != nil {
		return err
	}

	return nil
}

// note: removed needs to be unique here (i.e. both File and Address must be set).
func (ac *accountCache) delete(removed accounts.Account) {
	if err := ac.scanAccounts(); err != nil {
		ac.logger.Debug("can't scan account's", "err", err)
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	delete(ac.allMap, removed.Address)

	if err := ac.fileC.saveData(ac.allMap); err != nil {
		ac.logger.Debug("cant't save data in file,", "err", err)
	}
}

// find returns the cached account for address if there is a unique match.
// The exact matching rules are explained by the documentation of accounts.Account.
// Callers must hold ac.mu.
func (ac *accountCache) find(a accounts.Account) (accounts.Account, encryptedKeyJSONV3, error) {
	if err := ac.scanAccounts(); err != nil {
		return accounts.Account{}, encryptedKeyJSONV3{}, err
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	if encryptedKey, ok := ac.allMap[a.Address]; ok {
		return a, encryptedKey, nil
	}

	return accounts.Account{}, encryptedKeyJSONV3{}, accounts.ErrNoMatch
}

func (ac *accountCache) close() {
	ac.mu.Lock()

	if ac.throttle != nil {
		ac.throttle.Stop()
	}

	if ac.notify != nil {
		close(ac.notify)
		ac.notify = nil
	}

	ac.mu.Unlock()
}

// scanAccounts checks if any changes have occurred on the filesystem, and
// updates the account cache accordingly
func (ac *accountCache) scanAccounts() error {
	// Scan the entire folder metadata for file changes

	ac.mu.Lock()
	defer ac.mu.Unlock()

	accs, err := ac.fileC.scanOneFile()
	if err != nil {
		ac.logger.Debug("Failed to reload keystore contents", "err", err)

		return err
	}

	ac.allMap = make(map[types.Address]encryptedKeyJSONV3)

	for addr, key := range accs {
		ac.allMap[addr] = key
	}

	select {
	case ac.notify <- struct{}{}:
	default:
	}
	ac.logger.Trace("Handled keystore changes")

	return nil
}
