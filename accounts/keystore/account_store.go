package keystore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
)

// accountStore is a live index of all accounts in the keystore.
type accountStore struct {
	logger hclog.Logger
	keyDir string
	mu     sync.Mutex
	allMap map[types.Address]encryptedKey
}

func newAccountStore(keyDir string, logger hclog.Logger) (*accountStore, error) {
	ac := &accountStore{
		logger: logger,
		keyDir: keyDir,
		allMap: make(map[types.Address]encryptedKey),
	}

	if err := common.CreateDirSafe(keyDir, 0700); err != nil {
		ac.logger.Error("can't create dir", "err", err)

		return nil, fmt.Errorf("could not create keystore directory: %v", err)
	}

	keysPath := path.Join(keyDir, "keys.txt")

	ac.keyDir = keysPath

	if _, err := os.Stat(keysPath); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Create(keysPath); err != nil {
			ac.logger.Error("can't create new file", "err", err)

			return nil, fmt.Errorf("could not create keystore file: %v", err)
		}
	}

	if err := ac.readAccountsFromFile(); err != nil {
		return nil, fmt.Errorf("could not read keystore file: %v", err)
	}

	return ac, nil
}

func (ac *accountStore) accounts() []accounts.Account {
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

func (ac *accountStore) hasAddress(addr types.Address) bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	_, ok := ac.allMap[addr]

	return ok
}

func (ac *accountStore) add(newAccount accounts.Account, key encryptedKey) error {
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

func (ac *accountStore) update(account accounts.Account, newKey encryptedKey) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	var (
		oldKey encryptedKey
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
func (ac *accountStore) delete(removed accounts.Account) error {
	if err := ac.saveData(ac.allMap); err != nil {
		return fmt.Errorf("could not delete account: %w", err)
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	delete(ac.allMap, removed.Address)

	return nil
}

// find returns the cached account for address if there is a unique match.
func (ac *accountStore) find(a accounts.Account) (accounts.Account, encryptedKey, error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if encryptedKey, ok := ac.allMap[a.Address]; ok {
		return a, encryptedKey, nil
	}

	return accounts.Account{}, encryptedKey{}, accounts.ErrNoMatch
}

// readAccountsFromFile reads the keystore file and updates the account store.
func (ac *accountStore) readAccountsFromFile() error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	accs, err := ac.scanFile()
	if err != nil {
		ac.logger.Error("Failed to reload keystore contents", "err", err)

		return err
	}

	ac.allMap = make(map[types.Address]encryptedKey)

	for addr, key := range accs {
		ac.allMap[addr] = key
	}

	ac.logger.Trace("Handled keystore changes")

	return nil
}

func (ac *accountStore) saveData(accounts map[types.Address]encryptedKey) error {
	byteAccount, err := json.Marshal(accounts)
	if err != nil {
		return err
	}

	return common.SaveFileSafe(ac.keyDir, byteAccount, 0600)
}

func (ac *accountStore) scanFile() (map[types.Address]encryptedKey, error) {
	fi, err := os.ReadFile(ac.keyDir)
	if err != nil {
		return nil, err
	}

	if len(fi) == 0 {
		return nil, nil
	}

	var accounts = make(map[types.Address]encryptedKey)

	err = json.Unmarshal(fi, &accounts)
	if err != nil {
		return nil, err
	}

	return accounts, nil
}
