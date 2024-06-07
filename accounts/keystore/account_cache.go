package keystore

import (
	"errors"
	"os"
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
	fileC    fileCache
}

func newAccountCache(keyDir string, logger hclog.Logger) (*accountCache, chan struct{}) {
	ac := &accountCache{
		logger: logger,
		keydir: keyDir,
		notify: make(chan struct{}, 1),
	}

	keyPath := path.Join(keyDir, "keys.txt")

	ac.fileC = fileCache{all: make(map[types.Address]encryptedKeyJSONV3), keyDir: keyPath}

	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		os.Create(keyDir)
	}

	ac.scanAccounts()

	return ac, ac.notify
}

func (ac *accountCache) accounts() []accounts.Account {
	ac.scanAccounts()

	ac.mu.Lock()
	defer ac.mu.Unlock()

	cpy := make([]accounts.Account, len(ac.allMap))
	for addr := range ac.allMap {
		cpy = append(cpy, accounts.Account{Address: addr})
	}

	return cpy
}

func (ac *accountCache) hasAddress(addr types.Address) bool {
	ac.scanAccounts()

	ac.mu.Lock()
	defer ac.mu.Unlock()

	_, ok := ac.allMap[addr]

	return ok
}

func (ac *accountCache) add(newAccount accounts.Account) {
	ac.scanAccounts()

	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, ok := ac.allMap[newAccount.Address]; ok {
		return
	}

	ac.allMap[newAccount.Address] = encryptedKeyJSONV3{} // TO DO

	ac.fileC.saveData(ac.allMap)
}

// note: removed needs to be unique here (i.e. both File and Address must be set).
func (ac *accountCache) delete(removed accounts.Account) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.scanAccounts()

	delete(ac.allMap, removed.Address)

	ac.fileC.saveData(ac.allMap)
}

// find returns the cached account for address if there is a unique match.
// The exact matching rules are explained by the documentation of accounts.Account.
// Callers must hold ac.mu.
func (ac *accountCache) find(a accounts.Account) (accounts.Account, error) {
	ac.scanAccounts()

	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, ok := ac.allMap[a.Address]; ok {
		return a, nil
	}

	return accounts.Account{}, accounts.ErrNoMatch
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

	ac.notify <- struct{}{}

	ac.logger.Trace("Handled keystore changes")

	return nil
}
