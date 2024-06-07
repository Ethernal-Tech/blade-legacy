package keystore

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/google/uuid"
)

const (
	version = 3
)

type Key struct {
	ID uuid.UUID

	Address types.Address

	PrivateKey *ecdsa.PrivateKey
}

type keyStore interface {
	// Loads and decrypts the key from disk.
	GetKey(encryptedKey encryptedKeyJSONV3, auth string) (*Key, error)
	// Writes and encrypts the key.
	StoreKey(k *Key, auth string) (encryptedKeyJSONV3, error)
}

type encryptedKeyJSONV3 struct {
	Address string     `json:"address"`
	Crypto  CryptoJSON `json:"crypto"`
	ID      string     `json:"id"`
	Version int        `json:"version"`
}

type CryptoJSON struct {
	Cipher       string                 `json:"cipher"`
	CipherText   string                 `json:"ciphertext"`
	CipherParams cipherparamsJSON       `json:"cipherparams"`
	KDF          string                 `json:"kdf"`
	KDFParams    map[string]interface{} `json:"kdfparams"`
	MAC          string                 `json:"mac"`
}

type cipherparamsJSON struct {
	IV string `json:"iv"`
}

// TO DO newKeyFromECDSA
func newKeyFromECDSA(privateKeyECDSA *ecdsa.PrivateKey) *Key {
	id, err := uuid.NewRandom()
	if err != nil {
		panic(fmt.Sprintf("Could not create random uuid: %v", err)) //nolint:gocritic
	}

	key := &Key{
		ID:         id,
		Address:    crypto.PubKeyToAddress(&privateKeyECDSA.PublicKey), // TO DO get more time for this pointer
		PrivateKey: privateKeyECDSA,
	}

	return key
}

// keyFileName implements the naming convention for keyfiles:
// UTC--<created_at UTC ISO8601>-<address hex>
func keyFileName(keyAddr types.Address) string {
	ts := time.Now().UTC()

	return fmt.Sprintf("UTC--%s--%s", toISO8601(ts), hex.EncodeToString(keyAddr[:]))
}

func toISO8601(t time.Time) string {
	var tz string

	name, offset := t.Zone()

	if name == "UTC" {
		tz = "Z"
	} else {
		tz = fmt.Sprintf("%03d00", offset/3600)
	}

	return fmt.Sprintf("%04d-%02d-%02dT%02d-%02d-%02d.%09d%s",
		t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), tz)
}

func writeTemporaryKeyFile(file string, content []byte) (string, error) {
	// Create the keystore directory with appropriate permissions
	// in case it is not present yet.
	const dirPerm = 0700
	if err := os.MkdirAll(filepath.Dir(file), dirPerm); err != nil {
		return "", err
	}
	// Atomic write: create a temporary hidden file first
	// then move it into place. TempFile assigns mode 0600.
	f, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".tmp")
	if err != nil {
		return "", err
	}

	if _, err := f.Write(content); err != nil {
		f.Close()
		os.Remove(f.Name())

		return "", err
	}

	f.Close()

	return f.Name(), nil
}

func newKey(rand io.Reader) (*Key, error) {
	privateKeyECDSA, err := crypto.GenerateECDSAPrivateKey() // TO DO maybe not valid
	if err != nil {
		return nil, err
	}

	return newKeyFromECDSA(privateKeyECDSA), nil
}

func NewKeyForDirectICAP(rand io.Reader) *Key {
	randBytes := make([]byte, 64)
	_, err := rand.Read(randBytes)

	if err != nil {
		panic("key generation: could not read from random source: " + err.Error()) //nolint:gocritic
	}

	reader := bytes.NewReader(randBytes)

	privateKeyECDSA, err := ecdsa.GenerateKey(btcec.S256(), reader)
	if err != nil {
		panic("key generation: ecdsa.GenerateKey failed: " + err.Error()) //nolint:gocritic
	}

	key := newKeyFromECDSA(privateKeyECDSA)

	if !strings.HasPrefix(key.Address.String(), "0x00") {
		return NewKeyForDirectICAP(rand)
	}

	return key
}

func storeNewKey(ks keyStore, rand io.Reader, auth string) (encryptedKeyJSONV3, accounts.Account, error) {
	key, err := newKey(rand)
	if err != nil {
		return encryptedKeyJSONV3{}, accounts.Account{}, err
	}

	a := accounts.Account{
		Address: key.Address,
	}

	encryptedKey, err := ks.StoreKey(key, auth)
	if err != nil {
		zeroKey(key.PrivateKey)

		return encryptedKeyJSONV3{}, a, err
	}

	return encryptedKey, a, err
}

func writeKeyFile(file string, content []byte) error {
	name, err := writeTemporaryKeyFile(file, content)
	if err != nil {
		return err
	}

	return os.Rename(name, file)
}
