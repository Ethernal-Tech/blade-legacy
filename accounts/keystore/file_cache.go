package keystore

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/0xPolygon/polygon-edge/types"
)

// fileCache is a cache of files seen during scan of keystore.
type fileCache struct {
	all    map[types.Address]encryptedKeyJSONV3
	mu     sync.Mutex
	keyDir string
}

func NewFileCache(keyPath string) (*fileCache, error) {
	fc := &fileCache{all: make(map[types.Address]encryptedKeyJSONV3), keyDir: keyPath}

	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Create(fc.keyDir); err != nil {
			return nil, err
		}
	}

	return fc, nil
}

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
