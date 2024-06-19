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

func TestPassphraseEncryption(t *testing.T) {
	t.Parallel()

	passEncryption := &passphraseEncryption{veryLightScryptN, veryLightScryptP}

	k1, account, err := passEncryption.CreateNewKey(pass)
	require.NoError(t, err)

	k2, err := passEncryption.KeyDecrypt(k1, pass)
	require.NoError(t, err)

	require.Equal(t, types.StringToAddress(k1.Address), k2.Address)

	require.Equal(t, k2.Address, account.Address)
}

func TestPassphraseEncryptionDecryptionFail(t *testing.T) {
	t.Parallel()

	passEncryption := &passphraseEncryption{veryLightScryptN, veryLightScryptP}

	k1, _, err := passEncryption.CreateNewKey(pass)
	require.NoError(t, err)

	_, err = passEncryption.KeyDecrypt(k1, "bar")
	require.EqualError(t, err, ErrDecrypt.Error())
}

// Test and utils for the key store tests in the Ethereum JSON tests;
// testdataKeyStoreTests/basic_tests.json
type KeyStoreTest struct {
	EncryptedKey encryptedKey `json:"json"`
	Password     string       `json:"password"`
	Priv         string       `json:"priv"`
}

func Test_PBKDF2_1(t *testing.T) {
	t.Parallel()
	tests := loadKeyStoreTest(t, "testdata/test-keys.json")
	testDecrypt(t, tests["wikipage_test_vector_pbkdf2"])
}

var testsSubmodule = filepath.Join("..", "..", "tests", "testdata", "KeyStoreTests")

func skipIfSubmoduleMissing(t *testing.T) {
	t.Helper()

	if !common.FileExists(testsSubmodule) {
		t.Skipf("can't find JSON tests from submodule at %s", testsSubmodule)
	}
}

func Test_PBKDF2_2(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTest(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecrypt(t, tests["test1"])
}

func Test_PBKDF2_3(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTest(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecrypt(t, tests["python_generated_test_with_odd_iv"])
}

func Test_PBKDF2_4(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTest(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecrypt(t, tests["evilnonce"])
}

func Test_Scrypt_1(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTest(t, "testdata/test-keys.json")
	testDecrypt(t, tests["wikipage_test_vector_scrypt"])
}

func Test_Scrypt_2(t *testing.T) {
	skipIfSubmoduleMissing(t)
	t.Parallel()

	tests := loadKeyStoreTest(t, filepath.Join(testsSubmodule, "basic_tests.json"))
	testDecrypt(t, tests["test2"])
}

func testDecrypt(t *testing.T, test KeyStoreTest) {
	t.Helper()

	privBytes, _, err := decryptKey(&test.EncryptedKey, test.Password)
	require.NoError(t, err)

	privHex := hex.EncodeToString(privBytes)
	require.Equal(t, test.Priv, privHex)
}

func loadKeyStoreTest(t *testing.T, file string) map[string]KeyStoreTest {
	t.Helper()

	tests := make(map[string]KeyStoreTest)

	err := loadJSON(t, file, &tests)
	require.NoError(t, err)

	return tests
}

func TestKeyForDirectICAP(t *testing.T) {
	t.Parallel()

	key := NewKeyForDirectICAP(rand.Reader)
	require.True(t, strings.HasPrefix(key.Address.String(), "0x00"))
}

func Test_31_Byte_Key(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTest(t, "testdata/test-keys.json")
	testDecrypt(t, tests["31_byte_key"])
}

func Test_30_Byte_Key(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTest(t, "testdata/test-keys.json")
	testDecrypt(t, tests["30_byte_key"])
}

// Tests that a json key file can be decrypted and encrypted in multiple rounds.
func TestKeyEncryptDecrypt(t *testing.T) {
	t.Parallel()

	encrypted := new(encryptedKey)

	keyjson, err := os.ReadFile("testdata/light-test-key.json")
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal(keyjson, encrypted))

	password := ""
	address := types.StringToAddress("45dea0fb0bba44f4fcf290bba71fd57d7117cbb8")

	// Do a few rounds of decryption and encryption
	for i := 0; i < 3; i++ {
		// Try a bad password first
		_, err := DecryptKey(*encrypted, password+"bad")
		require.Error(t, err)
		// Decrypt with the correct password
		key, err := DecryptKey(*encrypted, password)
		require.NoError(t, err)

		require.Equal(t, address, key.Address)
		// Recrypt with a new password and start over
		password += "new data appended"
		*encrypted, err = EncryptKey(key, password, veryLightScryptN, veryLightScryptP)
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
