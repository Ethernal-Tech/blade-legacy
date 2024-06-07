package keystore

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

func tmpKeyStoreIface(t *testing.T, encrypted bool) (dir string, ks keyStore) {
	t.Helper()

	d := t.TempDir()

	if encrypted {
		ks = &keyStorePassphrase{d, veryLightScryptN, veryLightScryptP, true}
	} else {
		ks = &keyStorePlain{d}
	}

	return d, ks
}

func TestKeyStorePlain(t *testing.T) {
	t.Parallel()

	_, ks := tmpKeyStoreIface(t, false)

	pass := "" // not used but required by API

	k1, account, err := storeNewKey(ks, rand.Reader, pass)
	if err != nil {
		t.Fatal(err)
	}

	k2, err := ks.GetKey(k1.Address, account.URL.Path, pass)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(k1.Address, k2.Address) {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(k1.PrivateKey, k2.PrivateKey) {
		t.Fatal(err)
	}
}

func TestKeyStorePassphrase(t *testing.T) {
	t.Parallel()

	_, ks := tmpKeyStoreIface(t, true)

	k1, account, err := storeNewKey(ks, rand.Reader, pass)
	if err != nil {
		t.Fatal(err)
	}

	k2, err := ks.GetKey(k1.Address, account.URL.Path, pass)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(k1.Address, k2.Address) {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(k1.PrivateKey, k2.PrivateKey) {
		t.Fatal(err)
	}
}

func TestKeyStorePassphraseDecryptionFail(t *testing.T) {
	t.Parallel()

	_, ks := tmpKeyStoreIface(t, true)

	k1, account, err := storeNewKey(ks, rand.Reader, pass)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = ks.GetKey(k1.Address, account.URL.Path, "bar"); err.Error() != ErrDecrypt.Error() {
		t.Fatalf("wrong error for invalid password\ngot %q\nwant %q", err, ErrDecrypt)
	}
}

func TestImportPreSaleKey(t *testing.T) {
	t.Parallel()

	dir, ks := tmpKeyStoreIface(t, true)

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

	if !strings.HasPrefix(account.URL.Path, dir) {
		t.Errorf("imported account file not in keystore directory: %q", account.URL)
	}
}

// Test and utils for the key store tests in the Ethereum JSON tests;
// testdataKeyStoreTests/basic_tests.json
type KeyStoreTestV3 struct {
	JSON     encryptedKeyJSONV3
	Password string
	Priv     string
}

type KeyStoreTestV1 struct {
	JSON     encryptedKeyJSONV1
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

func TestV1_1(t *testing.T) {
	t.Parallel()

	tests := loadKeyStoreTestV1(t, "testdata/v1_test_vector.json")
	testDecryptV1(t, tests["test1"])
}

func TestV1_2(t *testing.T) {
	t.Parallel()

	ks := &keyStorePassphrase{"testdata/v1", LightScryptN, LightScryptP, true}
	addr := types.StringToAddress("cb61d5a9c4896fb9658090b597ef0e7be6f7b67e")
	file := "testdata/v1/cb61d5a9c4896fb9658090b597ef0e7be6f7b67e/cb61d5a9c4896fb9658090b597ef0e7be6f7b67e"

	k, err := ks.GetKey(addr, file, "g")
	if err != nil {
		t.Fatal(err)
	}

	privKey, err := crypto.MarshalECDSAPrivateKey(k.PrivateKey)
	require.NoError(t, err)

	privHex := hex.EncodeToString(privKey)

	expectedHex := "d1b1178d3529626a1a93e073f65028370d14c7eb0936eb42abef05db6f37ad7d"
	if privHex != expectedHex {
		t.Fatal(fmt.Errorf("Unexpected privkey: %v, expected %v", privHex, expectedHex))
	}
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

func testDecryptV1(t *testing.T, test KeyStoreTestV1) {
	t.Helper()

	privBytes, _, err := decryptKeyV1(&test.JSON, test.Password)
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

func loadKeyStoreTestV1(t *testing.T, file string) map[string]KeyStoreTestV1 {
	t.Helper()

	tests := make(map[string]KeyStoreTestV1)

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
