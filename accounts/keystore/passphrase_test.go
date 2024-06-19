package keystore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

const (
	veryLightScryptN = 2
	veryLightScryptP = 1
)

func TestKeyStorePassphrase(t *testing.T) {
	t.Parallel()

	ks := &keyStorePassphrase{veryLightScryptN, veryLightScryptP}

	k1, account, err := storeNewKey(ks, pass)
	require.NoError(t, err)

	k2, err := ks.KeyDecryption(k1, pass)
	require.NoError(t, err)

	require.Equal(t, types.StringToAddress(k1.Address), k2.Address)

	require.Equal(t, k2.Address, account.Address)
}

func TestKeyStorePassphraseDecryptionFail(t *testing.T) {
	t.Parallel()

	ks := &keyStorePassphrase{veryLightScryptN, veryLightScryptP}

	k1, _, err := storeNewKey(ks, pass)
	require.NoError(t, err)

	_, err = ks.KeyDecryption(k1, "bar")
	require.EqualError(t, err, ErrDecrypt.Error())
}

// Test and utils for the key store tests in the Ethereum JSON tests;
// testdataKeyStoreTests/basic_tests.json
type KeyStoreTestV3 struct {
	JSON     encryptedKeyJSONV3
	Password string
	Priv     string
}

func TestV3_PBKDF2_1(t *testing.T) {
	t.Parallel()
	tests := loadKeyStoreTestV3(t, "testdata/test-keys.json")
	testDecryptV3(t, tests["wikipage_test_vector_pbkdf2"])
}

var testsSubmodule = filepath.Join("..", "..", "tests", "testdata", "KeyStoreTests")

func skipIfSubmoduleMissing(t *testing.T) {
	t.Helper()

	if !common.FileExists(testsSubmodule) {
		t.Skipf("can't find JSON tests from submodule at %s", testsSubmodule)
	}
}

func TestV3_PBKDF2_2(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTestV3(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecryptV3(t, tests["test1"])
}

func TestV3_PBKDF2_3(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTestV3(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecryptV3(t, tests["python_generated_test_with_odd_iv"])
}

func TestV3_PBKDF2_4(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTestV3(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecryptV3(t, tests["evilnonce"])
}

func TestV3_Scrypt_1(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTestV3(t, "testdata/test-keys.json")
	testDecryptV3(t, tests["wikipage_test_vector_scrypt"])
}

func TestV3_Scrypt_2(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTestV3(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecryptV3(t, tests["test2"])
}

func testDecryptV3(t *testing.T, test KeyStoreTestV3) {
	t.Helper()

	privBytes, _, err := decryptKeyV3(&test.JSON, test.Password)
	require.NoError(t, err)

	privHex := hex.EncodeToString(privBytes)
	require.Equal(t, test.Priv, privHex)
}

func loadKeyStoreTestV3(t *testing.T, file string) map[string]KeyStoreTestV3 {
	t.Helper()

	tests := make(map[string]KeyStoreTestV3)

	err := loadJSON(t, file, &tests)
	require.NoError(t, err)

	return tests
}

func TestKeyForDirectICAP(t *testing.T) {
	t.Parallel()

	key := NewKeyForDirectICAP(rand.Reader)
	require.True(t, strings.HasPrefix(key.Address.String(), "0x00"))
}

func TestV3_31_Byte_Key(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTestV3(t, "testdata/test-keys.json")
	testDecryptV3(t, tests["31_byte_key"])
}

func TestV3_30_Byte_Key(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTestV3(t, "testdata/test-keys.json")
	testDecryptV3(t, tests["30_byte_key"])
}

// Tests that a json key file can be decrypted and encrypted in multiple rounds.
func TestKeyEncryptDecrypt(t *testing.T) {
	t.Parallel()

	keyEncrypted := new(encryptedKeyJSONV3)

	keyjson, err := os.ReadFile("testdata/light-test-key.json")
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal(keyjson, keyEncrypted))

	password := ""
	address := types.StringToAddress("45dea0fb0bba44f4fcf290bba71fd57d7117cbb8")

	// Do a few rounds of decryption and encryption
	for i := 0; i < 3; i++ {
		// Try a bad password first
		_, err := DecryptKey(*keyEncrypted, password+"bad")
		require.Error(t, err)
		// Decrypt with the correct password
		key, err := DecryptKey(*keyEncrypted, password)
		require.NoError(t, err)

		require.Equal(t, address, key.Address)
		// Recrypt with a new password and start over
		password += "new data appended"
		*keyEncrypted, err = EncryptKey(key, password, veryLightScryptN, veryLightScryptP)
		require.NoError(t, err)
	}
}

func loadJSON(t *testing.T, file string, val interface{}) error {
	t.Helper()

	content, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(content, val); err != nil {
		if syntaxerr, ok := err.(*json.SyntaxError); ok { //nolint:errorlint
			line := findLine(t, content, syntaxerr.Offset)

			return fmt.Errorf("JSON syntax error at %v:%v: %w", file, line, err)
		}

		return fmt.Errorf("JSON unmarshal error in %v: %w", file, err)
	}

	return nil
}

func findLine(t *testing.T, data []byte, offset int64) (line int) {
	t.Helper()

	line = 1

	for i, r := range string(data) {
		if int64(i) >= offset {
			return
		}

		if r == '\n' {
			line++
		}
	}

	return
}
