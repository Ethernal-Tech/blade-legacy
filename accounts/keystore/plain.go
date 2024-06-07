package keystore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/google/uuid"
)

type keyStorePlain struct {
	keysDirPath string
}

func (ks keyStorePlain) GetKey(addr types.Address, filename, auth string) (*Key, error) {
	fd, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	key := new(Key)

	defer fd.Close()

	stat, err := fd.Stat()
	if err != nil {
		return nil, err
	}

	var (
		dat      map[string]interface{}
		jsonData = make([]byte, stat.Size())
	)

	_, err = fd.Read(jsonData)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(jsonData, &dat); err != nil {
		return nil, err
	}

	key.Address = types.StringToAddress(dat["address"].(string)) //nolint:forcetypeassert

	key.ID = uuid.MustParse(dat["id"].(string)) //nolint:forcetypeassert

	key.PrivateKey, err = crypto.BytesToECDSAPrivateKey([]byte(dat["privatekey"].(string)))
	if err != nil {
		return nil, err
	}

	if key.Address != addr {
		return nil, fmt.Errorf("key content mismatch: have address %x, want %x", key.Address, addr)
	}

	return key, nil
}

func (ks keyStorePlain) StoreKey(filename string, key *Key, auth string) error {
	content, err := json.Marshal(key)
	if err != nil {
		return err
	}

	return writeKeyFile(filename, content)
}

func (ks keyStorePlain) JoinPath(filename string) string {
	if filepath.IsAbs(filename) {
		return filename
	}

	return filepath.Join(ks.keysDirPath, filename)
}
