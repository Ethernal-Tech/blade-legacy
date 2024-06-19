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

type keyEncryption interface {
	// decrypts key and return non crypted key
	KeyDecrypt(encrypted encryptedKey, auth string) (*Key, error)
	// get non crypted key and do encryption
	KeyEncrypt(k *Key, auth string) (encryptedKey, error)

	StoreNewKey(auth string) (encryptedKey, accounts.Account, error)
}

type encryptedKey struct {
	Address string `json:"address"`
	Crypto  Crypto `json:"crypto"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type Crypto struct {
	Cipher       string                 `json:"cipher"`
	CipherText   string                 `json:"ciphertext"`
	CipherParams cipherParams           `json:"cipherparams"`
	KDF          string                 `json:"kdf"`
	KDFParams    map[string]interface{} `json:"kdfparams"`
	MAC          string                 `json:"mac"`
}

type cipherParams struct {
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
