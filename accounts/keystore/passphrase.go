package keystore

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

const (
	keyHeaderKDF = "scrypt"

	// StandardScryptN is the N parameter of Scrypt encryption algorithm, using 256MB
	// memory and taking approximately 1s CPU time on a modern processor.
	StandardScryptN = 1 << 18

	// StandardScryptP is the P parameter of Scrypt encryption algorithm, using 256MB
	// memory and taking approximately 1s CPU time on a modern processor.
	StandardScryptP = 1

	// LightScryptN is the N parameter of Scrypt encryption algorithm, using 4MB
	// memory and taking approximately 100ms CPU time on a modern processor.
	LightScryptN = 1 << 12

	// LightScryptP is the P parameter of Scrypt encryption algorithm, using 4MB
	// memory and taking approximately 100ms CPU time on a modern processor.
	LightScryptP = 6

	scryptR     = 8
	scryptDKLen = 32
)

type keyStorePassphrase struct {
	scryptN int
	scryptP int
	// skipKeyFileVerification disables the security-feature which does
	// reads and decrypts any newly created keyfiles. This should be 'false' in all
	// cases except tests -- setting this to 'true' is not recommended.
}

func (ks keyStorePassphrase) GetKey(encryptedKey encryptedKeyJSONV3, auth string) (*Key, error) {
	key, err := DecryptKey(encryptedKey, auth)
	if err != nil {
		return nil, err
	}

	if key.Address != types.StringToAddress(encryptedKey.Address) {
		return nil, fmt.Errorf("key content mismatch: have account %x, want %x", key.Address, encryptedKey.Address)
	}

	return key, nil
}

// StoreKey generates a key, encrypts with 'auth' and stores in the given directory
func StoreKey(auth string, scryptN, scryptP int) (accounts.Account, error) {
	_, a, err := storeNewKey(&keyStorePassphrase{scryptN, scryptP}, rand.Reader, auth)

	return a, err
}

func (ks keyStorePassphrase) StoreKey(key *Key, auth string) (encryptedKeyJSONV3, error) {
	encryptedKey, err := EncryptKey(key, auth, ks.scryptN, ks.scryptP)
	if err != nil {
		return encryptedKeyJSONV3{}, err
	}

	return encryptedKey, nil
}

// EncryptDataV3 encrypts the data given as 'data' with the password 'auth'.
func EncryptDataV3(data, auth []byte, scryptN, scryptP int) (CryptoJSON, error) {
	salt := make([]byte, 32)

	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		panic("reading from crypto/rand failed: " + err.Error()) //nolint:gocritic
	}

	derivedKey, err := scrypt.Key(auth, salt, scryptN, scryptR, scryptP, scryptDKLen)
	if err != nil {
		return CryptoJSON{}, err
	}

	encryptKey := derivedKey[:16]

	iv := make([]byte, aes.BlockSize) // 16
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		panic("reading from crypto/rand failed: " + err.Error()) //nolint:gocritic
	}

	cipherText, err := aesCTRXOR(encryptKey, data, iv)
	if err != nil {
		return CryptoJSON{}, err
	}

	mac := crypto.Keccak256(derivedKey[16:32], cipherText)

	scryptParamsJSON := make(map[string]interface{}, 5)
	scryptParamsJSON["n"] = scryptN
	scryptParamsJSON["r"] = scryptR
	scryptParamsJSON["p"] = scryptP
	scryptParamsJSON["dklen"] = scryptDKLen
	scryptParamsJSON["salt"] = hex.EncodeToString(salt)
	cipherParamsJSON := cipherparamsJSON{
		IV: hex.EncodeToString(iv),
	}

	cryptoStruct := CryptoJSON{
		Cipher:       "aes-128-ctr",
		CipherText:   hex.EncodeToString(cipherText),
		CipherParams: cipherParamsJSON,
		KDF:          keyHeaderKDF,
		KDFParams:    scryptParamsJSON,
		MAC:          hex.EncodeToString(mac),
	}

	return cryptoStruct, nil
}

// EncryptKey encrypts a key using the specified scrypt parameters into a json
// blob that can be decrypted later on.
func EncryptKey(key *Key, auth string, scryptN, scryptP int) (encryptedKeyJSONV3, error) {
	keyBytes, err := crypto.MarshalECDSAPrivateKey(key.PrivateKey) // TO DO maybe wrong
	if err != nil {
		return encryptedKeyJSONV3{}, err
	}

	cryptoStruct, err := EncryptDataV3(keyBytes, []byte(auth), scryptN, scryptP)
	if err != nil {
		return encryptedKeyJSONV3{}, err
	}

	encryptedKeyJSONV3 := encryptedKeyJSONV3{
		hex.EncodeToString(key.Address[:]),
		cryptoStruct,
		key.ID.String(),
		version,
	}

	return encryptedKeyJSONV3, nil
}

// DecryptKey decrypts a key from a json blob, returning the private key itself.
func DecryptKey(encryptedKey encryptedKeyJSONV3, auth string) (*Key, error) {
	// Parse the json into a simple map to fetch the key version
	keyBytes, keyID, err := decryptKeyV3(&encryptedKey, auth)
	if err != nil {
		return nil, err
	}

	key, err := crypto.DToECDSA(keyBytes, true) // TO DO maybe wrong
	if err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	id, err := uuid.FromBytes(keyID)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID: %w", err)
	}

	return &Key{
		ID:         id,
		Address:    crypto.PubKeyToAddress(&key.PublicKey),
		PrivateKey: key,
	}, nil
}

func DecryptDataV3(cryptoJSON CryptoJSON, auth string) ([]byte, error) {
	if cryptoJSON.Cipher != "aes-128-ctr" {
		return nil, fmt.Errorf("cipher not supported: %v", cryptoJSON.Cipher)
	}

	mac, err := hex.DecodeString(cryptoJSON.MAC)
	if err != nil {
		return nil, err
	}

	iv, err := hex.DecodeString(cryptoJSON.CipherParams.IV)
	if err != nil {
		return nil, err
	}

	cipherText, err := hex.DecodeString(cryptoJSON.CipherText)
	if err != nil {
		return nil, err
	}

	derivedKey, err := getKDFKey(cryptoJSON, auth)
	if err != nil {
		return nil, err
	}

	calculatedMAC := crypto.Keccak256(derivedKey[16:32], cipherText)
	if !bytes.Equal(calculatedMAC, mac) {
		return nil, accounts.ErrDecrypt
	}

	plainText, err := aesCTRXOR(derivedKey[:16], cipherText, iv)
	if err != nil {
		return nil, err
	}

	return plainText, err
}

func decryptKeyV3(keyProtected *encryptedKeyJSONV3, auth string) (keyBytes []byte, keyID []byte, err error) {
	if keyProtected.Version != version {
		return nil, nil, fmt.Errorf("version not supported: %v", keyProtected.Version)
	}

	keyUUID, err := uuid.Parse(keyProtected.ID)
	if err != nil {
		return nil, nil, err
	}

	keyID = keyUUID[:]

	plainText, err := DecryptDataV3(keyProtected.Crypto, auth)
	if err != nil {
		return nil, nil, err
	}

	return plainText, keyID, err
}

func getKDFKey(cryptoJSON CryptoJSON, auth string) ([]byte, error) {
	authArray := []byte(auth)

	salt, err := hex.DecodeString(cryptoJSON.KDFParams["salt"].(string))
	if err != nil {
		return nil, err
	}

	dkLen := ensureInt(cryptoJSON.KDFParams["dklen"])

	if cryptoJSON.KDF == keyHeaderKDF {
		n := ensureInt(cryptoJSON.KDFParams["n"])
		r := ensureInt(cryptoJSON.KDFParams["r"])
		p := ensureInt(cryptoJSON.KDFParams["p"])

		return scrypt.Key(authArray, salt, n, r, p, dkLen)
	} else if cryptoJSON.KDF == "pbkdf2" {
		c := ensureInt(cryptoJSON.KDFParams["c"])
		prf := cryptoJSON.KDFParams["prf"].(string) //nolint:forcetypeassert

		if prf != "hmac-sha256" {
			return nil, fmt.Errorf("unsupported PBKDF2 PRF: %s", prf)
		}

		key := pbkdf2.Key(authArray, salt, c, dkLen, sha256.New)

		return key, nil
	}

	return nil, fmt.Errorf("unsupported KDF: %s", cryptoJSON.KDF)
}

func ensureInt(x interface{}) int {
	res, ok := x.(int)
	if !ok {
		res = int(x.(float64)) //nolint:forcetypeassert
	}

	return res
}
