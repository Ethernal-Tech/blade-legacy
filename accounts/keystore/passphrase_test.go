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

	"github.com/0xPolygon/polygon-edge/accounts"
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

	k1, account, err := storeNewKey(ks, rand.Reader, pass)
	require.NoError(t, err)

	k2, err := ks.GetKey(k1, pass)
	require.NoError(t, err)

	require.Equal(t, types.StringToAddress(k1.Address), k2.Address)

	require.Equal(t, k2.Address, account.Address)
}

func TestKeyStorePassphraseDecryptionFail(t *testing.T) {
	t.Parallel()

	ks := &keyStorePassphrase{veryLightScryptN, veryLightScryptP}

	k1, _, err := storeNewKey(ks, rand.Reader, pass)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = ks.GetKey(k1, "bar"); err.Error() != ErrDecrypt.Error() {
		t.Fatalf("wrong error for invalid password\ngot %q\nwant %q", err, ErrDecrypt)
	}
}

func TestImportPreSaleKey(t *testing.T) {
	t.Parallel()

	ks := &keyStorePassphrase{veryLightScryptN, veryLightScryptP}

	// file content of a presale key file generated with:
	// python pyethsaletool.py genwallet
	// with password "foo"
	fileContent := "{\"encseed\": \"26d87f5f2bf9835f9a47eefae571bc09f9107bb13d54ff12a4ec095d01f83897494cf34f7bed2ed34126ecba9db7b62de56c9d7cd136520a0427bfb11b8954ba7ac39b90d4650d3448e31185affcd74226a68f1e94b1108e6e0a4a91cdd83eba\", \"ethaddr\": \"d4584b5f6229b7be90727b0fc8c6b91bb427821f\", \"email\": \"gustav.simonsson@gmail.com\", \"btcaddr\": \"1EVknXyFC68kKNLkh6YnKzW41svSRoaAcx\"}"

	account, _, err := importPreSaleKey(ks, []byte(fileContent), pass)
	if err != nil {
		t.Fatal(err)
	}

	if account.Address != types.StringToAddress("d4584b5f6229b7be90727b0fc8c6b91bb427821f") {
		t.Errorf("imported account has wrong address %x", account.Address)
	}
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
	tests := loadKeyStoreTestV3(t, "testdata/v3_test_vector.json")
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

	tests := loadKeyStoreTestV3(t, "testdata/v3_test_vector.json")
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
	if err != nil {
		t.Fatal(err)
	}

	privHex := hex.EncodeToString(privBytes)
	if test.Priv != privHex {
		t.Fatal(fmt.Errorf("Decrypted bytes not equal to test, expected %v have %v", test.Priv, privHex))
	}
}

func loadKeyStoreTestV3(t *testing.T, file string) map[string]KeyStoreTestV3 {
	t.Helper()

	tests := make(map[string]KeyStoreTestV3)

	err := accounts.LoadJSON(file, &tests)
	if err != nil {
		t.Fatal(err)
	}

	return tests
}

func TestKeyForDirectICAP(t *testing.T) {
	t.Parallel()

	key := NewKeyForDirectICAP(rand.Reader)
	if !strings.HasPrefix(key.Address.String(), "0x00") {
		t.Errorf("Expected first address byte to be zero, have: %s", key.Address.String())
	}
}

func TestV3_31_Byte_Key(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTestV3(t, "testdata/v3_test_vector.json")
	testDecryptV3(t, tests["31_byte_key"])
}

func TestV3_30_Byte_Key(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTestV3(t, "testdata/v3_test_vector.json")
	testDecryptV3(t, tests["30_byte_key"])
}

// Tests that a json key file can be decrypted and encrypted in multiple rounds.
func TestKeyEncryptDecrypt(t *testing.T) {
	t.Parallel()
	keyEncrypted := new(encryptedKeyJSONV3)

	keyjson, err := os.ReadFile("testdata/very-light-scrypt.json")
	if err != nil {
		t.Fatal(err)
	}

	json.Unmarshal(keyjson, keyEncrypted)

	password := ""
	address := types.StringToAddress("45dea0fb0bba44f4fcf290bba71fd57d7117cbb8")

	// Do a few rounds of decryption and encryption
	for i := 0; i < 3; i++ {
		// Try a bad password first
		if _, err := DecryptKey(*keyEncrypted, password+"bad"); err == nil {
			t.Errorf("test %d: json key decrypted with bad password", i)
		}
		// Decrypt with the correct password
		key, err := DecryptKey(*keyEncrypted, password)
		if err != nil {
			t.Fatalf("test %d: json key failed to decrypt: %v", i, err)
		}

		if key.Address != address {
			t.Errorf("test %d: key address mismatch: have %x, want %x", i, key.Address, address)
		}
		// Recrypt with a new password and start over
		password += "new data appended"
		if *keyEncrypted, err = EncryptKey(key, password, veryLightScryptN, veryLightScryptP); err != nil {
			t.Errorf("test %d: failed to re-encrypt key %v", i, err)
		}
	}
}