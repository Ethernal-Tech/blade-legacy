package keystore

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/types"
)

// fileCache is a cache of files seen during scan of keystore.
type fileCache struct {
	all     map[types.Address]encryptedKeyJSONV3 // Set of all files from the keystore folder
	lastMod time.Time                            // Last time instance when a file was modified
	mu      sync.Mutex
	keyDir  string
}

/*func (fc *fileCache) scan(keyDir string) (mapset.Set[string], mapset.Set[string], mapset.Set[string], error) {
	// List all the files from the keystore folder
	files, err := os.ReadDir(keyDir)
	if err != nil {
		return nil, nil, nil, err
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	all := mapset.NewThreadUnsafeSet[string]()
	mods := mapset.NewThreadUnsafeSet[string]()

	var newLastMod time.Time

	for _, fi := range files {
		if nonKeyFile(fi) {
			continue
		}

		path := filepath.Join(keyDir, fi.Name())

		all.Add(path)

		info, err := fi.Info()
		if err != nil {
			return nil, nil, nil, err
		}

		modified := info.ModTime()
		if modified.After(fc.lastMod) {
			mods.Add(path)
		}

		if modified.After(newLastMod) {
			newLastMod = modified
		}
	}

	deletes := fc.all.Difference(all)
	creates := all.Difference(fc.all)
	updates := mods.Difference(creates)

	fc.all, fc.lastMod = all, newLastMod

	return creates, deletes, updates, nil
}  */

func (fc *fileCache) saveData(accounts map[types.Address]encryptedKeyJSONV3) error {
	fi, err := os.Create(fc.keyDir)
	if err != nil {
		return err
	}

	defer fi.Close()

	byteAccount, err := json.Marshal(accounts)
	if err != nil {
		return err
	}

	if _, err := fi.Write(byteAccount); err != nil {
		return err
	}

	return nil
}

func (fc *fileCache) scanOneFile() (map[types.Address]encryptedKeyJSONV3, error) {
	fi, err := os.ReadFile(fc.keyDir)
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

	fc.all = accounts

	return accounts, nil
}
