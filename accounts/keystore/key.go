package keystore

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"io"
	"strings"

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
	ID         uuid.UUID
	Address    types.Address
	PrivateKey *ecdsa.PrivateKey
}

type keyStore interface {
	// decrypts key and return non crypted key
	KeyDecryption(encryptedKey encryptedKeyJSONV3, auth string) (*Key, error)
	// get non crypted key and do encryption
	KeyEncryption(k *Key, auth string) (encryptedKeyJSONV3, error)
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

// return new key
func newKeyFromECDSA(privateKeyECDSA *ecdsa.PrivateKey) *Key {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil
	}

	key := &Key{
		ID:         id,
		Address:    crypto.PubKeyToAddress(&privateKeyECDSA.PublicKey), // TO DO get more time for this pointer
		PrivateKey: privateKeyECDSA,
	}

	return key
}

func newKey() (*Key, error) {
	privateKeyECDSA, err := crypto.GenerateECDSAPrivateKey() // TO DO maybe not valid
	if err != nil {
		return nil, err
	}

	key := newKeyFromECDSA(privateKeyECDSA)
	if key == nil {
		return nil, fmt.Errorf("can't create key")
	}

	return key, nil
}

func NewKeyForDirectICAP(rand io.Reader) *Key {
	randBytes := make([]byte, 64)
	_, err := rand.Read(randBytes)

	if err != nil {
		return nil
	}

	reader := bytes.NewReader(randBytes)

	privateKeyECDSA, err := ecdsa.GenerateKey(btcec.S256(), reader)
	if err != nil {
		return nil
	}

	key := newKeyFromECDSA(privateKeyECDSA)
	if key == nil {
		return nil
	}

	if !strings.HasPrefix(key.Address.String(), "0x00") {
		return NewKeyForDirectICAP(rand)
	}

	return key
}

func storeNewKey(ks keyStore, auth string) (encryptedKeyJSONV3, accounts.Account, error) {
	key, err := newKey()
	if err != nil {
		return encryptedKeyJSONV3{}, accounts.Account{}, err
	}

	a := accounts.Account{
		Address: key.Address,
	}

	encryptedKey, err := ks.KeyEncryption(key, auth)
	if err != nil {
		zeroKey(key.PrivateKey)

		return encryptedKeyJSONV3{}, a, err
	}

	return encryptedKey, a, err
}
