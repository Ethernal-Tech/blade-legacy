package keystore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
)

// accountCache is a live index of all accounts in the keystore.
type accountCache struct {
	logger   hclog.Logger
	keyDir   string
	mu       sync.Mutex
	allMap   map[types.Address]encryptedKeyJSONV3
	throttle *time.Timer
	notify   chan struct{}
}

func newAccountCache(keyDir string, logger hclog.Logger) (*accountCache, chan struct{}) {
	ac := &accountCache{
		logger: logger,
		keyDir: keyDir,
		notify: make(chan struct{}, 1),
		allMap: make(map[types.Address]encryptedKeyJSONV3),
	}

	if err := common.CreateDirSafe(keyDir, 0700); err != nil {
		ac.logger.Error("can't create dir", "err", err)

		return nil, nil
	}

	keysPath := path.Join(keyDir, "keys.txt")

	ac.keyDir = keysPath

	if _, err := os.Stat(keysPath); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Create(keysPath); err != nil {
			ac.logger.Error("can't create new file", "err", err)

			return nil, nil
		}
	}

	ac.scanAccounts() //nolint:errcheck

	return ac, ac.notify
}

func (ac *accountCache) accounts() []accounts.Account {
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
	ac.mu.Lock()
	defer ac.mu.Unlock()

	_, ok := ac.allMap[addr]

	return ok
}

func (ac *accountCache) add(newAccount accounts.Account, key encryptedKeyJSONV3) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, ok := ac.allMap[newAccount.Address]; ok {
		return errors.New("account already exists")
	}

	ac.allMap[newAccount.Address] = key

	if err := ac.saveData(ac.allMap); err != nil {
		// if we can't save the data, we should remove the account from the map
		delete(ac.allMap, newAccount.Address)

		return err
	}

	return nil
}

func (ac *accountCache) update(account accounts.Account, newKey encryptedKeyJSONV3) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	var (
		oldKey encryptedKeyJSONV3
		ok     bool
	)

	if oldKey, ok = ac.allMap[account.Address]; !ok {
		return fmt.Errorf("account: %s doesn't exists", account.Address.String())
	} else {
		ac.allMap[account.Address] = newKey
	}

	if err := ac.saveData(ac.allMap); err != nil {
		// if we can't save the data, we should return the old key to the map
		ac.allMap[account.Address] = oldKey

		return err
	}

	return nil
}

// note: removed needs to be unique here (i.e. both File and Address must be set).
func (ac *accountCache) delete(removed accounts.Account) error {
	if err := ac.saveData(ac.allMap); err != nil {
		return fmt.Errorf("could not delete account: %w", err)
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	delete(ac.allMap, removed.Address)

	return nil
}

// find returns the cached account for address if there is a unique match.
// The exact matching rules are explained by the documentation of accounts.Account.
// Callers must hold ac.mu.
func (ac *accountCache) find(a accounts.Account) (accounts.Account, encryptedKeyJSONV3, error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if encryptedKey, ok := ac.allMap[a.Address]; ok {
		return a, encryptedKey, nil
	}

	return accounts.Account{}, encryptedKeyJSONV3{}, accounts.ErrNoMatch
}

func (ac *accountCache) close() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.throttle != nil {
		ac.throttle.Stop()
	}

	if ac.notify != nil {
		close(ac.notify)
		ac.notify = nil
	}
}

// scanAccounts refresh data of  account map
func (ac *accountCache) scanAccounts() error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	accs, err := ac.scanFile()
	if err != nil {
		ac.logger.Error("Failed to reload keystore contents", "err", err)

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

func (ac *accountCache) saveData(accounts map[types.Address]encryptedKeyJSONV3) error {
	byteAccount, err := json.Marshal(accounts)
	if err != nil {
		return err
	}

	return common.SaveFileSafe(ac.keyDir, byteAccount, 0600)
}

func (ac *accountCache) scanFile() (map[types.Address]encryptedKeyJSONV3, error) {
	fi, err := os.ReadFile(ac.keyDir)
	if err != nil {
		return nil, err
	}

	if len(fi) == 0 {
		return nil, nil
	}

	var accounts = make(map[types.Address]encryptedKeyJSONV3)

	err = json.Unmarshal(fi, &accounts)
	if err != nil {
		return nil, err
	}

	return accounts, nil
}
