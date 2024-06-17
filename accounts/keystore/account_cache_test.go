package keystore

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	cachetestDir, _   = filepath.Abs(filepath.Join("testdata", "keystore"))
	cachetestAccounts = []accounts.Account{
		{
			Address: types.StringToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"),
		},
		{
			Address: types.StringToAddress("f466859ead1932d743d622cb74fc058882e8648a"),
		},
		{
			Address: types.StringToAddress("289d485d9771714cce91d3393d764e1311907acc"),
		},
	}
)

func TestCacheInitialReload(t *testing.T) {
	t.Parallel()

	cache, _ := newAccountCache(cachetestDir, hclog.NewNullLogger())
	accs := cache.accounts()

	require.Equal(t, 3, len(accs))

	for _, acc := range cachetestAccounts {
		require.True(t, cache.hasAddress(acc.Address))
	}

	unwantedAccount := accounts.Account{Address: types.StringToAddress("2cac1adea150210703ba75ed097ddfe24e14f213")}

	require.False(t, cache.hasAddress(unwantedAccount.Address))
}

func TestCacheAddDelete(t *testing.T) {
	t.Parallel()

	tDir := t.TempDir()

	cache, _ := newAccountCache(tDir, hclog.NewNullLogger())

	accs := []accounts.Account{
		{
			Address: types.StringToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		},
		{
			Address: types.StringToAddress("2cac1adea150210703ba75ed097ddfe24e14f213"),
		},
		{
			Address: types.StringToAddress("8bda78331c916a08481428e4b07c96d3e916d165"),
		},
		{
			Address: types.StringToAddress("d49ff4eeb0b2686ed89c0fc0f2b6ea533ddbbd5e"),
		},
		{
			Address: types.StringToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"),
		},
		{
			Address: types.StringToAddress("f466859ead1932d743d622cb74fc058882e8648a"),
		},
		{
			Address: types.StringToAddress("289d485d9771714cce91d3393d764e1311907acc"),
		},
	}

	for _, a := range accs {
		require.NoError(t, cache.add(a, encryptedKeyJSONV3{}))
	}
	// Add some of them twice to check that they don't get reinserted.
	require.Error(t, cache.add(accs[0], encryptedKeyJSONV3{}))
	require.Error(t, cache.add(accs[2], encryptedKeyJSONV3{}))

	// Check that the account list is sorted by filename.
	wantAccounts := make([]accounts.Account, len(accs))

	copy(wantAccounts, accs)

	for _, a := range accs {
		require.True(t, cache.hasAddress(a.Address))
	}

	// Expected to return false because this address is not contained in cache
	require.False(t, cache.hasAddress(types.StringToAddress("fd9bd350f08ee3c0c19b85a8e16114a11a60aa4e")))

	// Delete a few keys from the cache.
	for i := 0; i < len(accs); i += 2 {
		require.NoError(t, cache.delete(wantAccounts[i]))
	}

	require.NoError(t, cache.delete(accounts.Account{Address: types.StringToAddress("fd9bd350f08ee3c0c19b85a8e16114a11a60aa4e")}))

	// Check content again after deletion.
	wantAccountsAfterDelete := []accounts.Account{
		wantAccounts[1],
		wantAccounts[3],
		wantAccounts[5],
	}

	deletedAccounts := []accounts.Account{
		wantAccounts[0],
		wantAccounts[2],
		wantAccounts[4],
	}

	for _, acc := range wantAccountsAfterDelete {
		require.True(t, cache.hasAddress(acc.Address))
	}

	for _, acc := range deletedAccounts {
		require.False(t, cache.hasAddress(acc.Address))
	}
}

func TestCacheFind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	cache, _ := newAccountCache(dir, hclog.NewNullLogger())

	accs := []accounts.Account{
		{
			Address: types.StringToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		},
		{
			Address: types.StringToAddress("2cac1adea150210703ba75ed097ddfe24e14f213"),
		},
		{
			Address: types.StringToAddress("d49ff4eeb0b2686ed89c0fc0f2b6ea533ddbbd5e"),
		},
	}

	matchAccount := accounts.Account{
		Address: types.StringToAddress("d49ff4eeb0b2686ed89c0fc0f2b6ea533ddbbd5e"),
	}

	for _, acc := range accs {
		require.NoError(t, cache.add(acc, encryptedKeyJSONV3{}))
	}

	require.Error(t, cache.add(matchAccount, encryptedKeyJSONV3{}))

	nomatchAccount := accounts.Account{
		Address: types.StringToAddress("f466859ead1932d743d622cb74fc058882e8648a"),
	}

	tests := []struct {
		Query      accounts.Account
		WantResult accounts.Account
		WantError  error
	}{
		// by address
		{Query: accounts.Account{Address: accs[0].Address}, WantResult: accs[0]},
		// by file and address
		{Query: accs[0], WantResult: accs[0]},
		// ambiguous address, tie resolved by file
		{Query: accs[2], WantResult: accs[2]},
		// no match error
		{Query: nomatchAccount, WantError: accounts.ErrNoMatch},
		{Query: accounts.Account{Address: nomatchAccount.Address}, WantError: accounts.ErrNoMatch},
	}

	for i, test := range tests {
		a, _, err := cache.find(test.Query)

		assert.Equal(t, test.WantError, err, fmt.Sprintf("Error mismatch test %d", i))

		assert.Equal(t, test.WantResult, a, fmt.Sprintf("Not same result %d", i))
	}
}

// TestUpdatedKeyfileContents tests that updating the contents of a keystore file
// is noticed by the watcher, and the account cache is updated accordingly
func TestCacheUpdate(t *testing.T) {
	t.Parallel()

	keyDir := t.TempDir()

	accountCache, _ := newAccountCache(keyDir, hclog.NewNullLogger())

	list := accountCache.accounts()
	if len(list) > 0 {
		t.Error("initial account list not empty:", list)
	}

	listOfEncryptedKeys := make([]encryptedKeyJSONV3, len(cachetestAccounts))

	for i, acc := range cachetestAccounts {
		encryptKey := encryptedKeyJSONV3{Address: acc.Address.String(), Crypto: CryptoJSON{Cipher: fmt.Sprintf("test%d", i), CipherText: fmt.Sprintf("test%d", i)}}
		listOfEncryptedKeys[i] = encryptKey

		require.NoError(t, accountCache.add(acc, encryptKey))
	}

	require.NoError(t, accountCache.update(cachetestAccounts[0], encryptedKeyJSONV3{Address: cachetestAccounts[0].Address.String(), Crypto: CryptoJSON{Cipher: "test", CipherText: "test"}}))

	wantAccount, encryptedKey, err := accountCache.find(cachetestAccounts[0])
	require.NoError(t, err)

	require.Equal(t, wantAccount.Address.String(), encryptedKey.Address)

	require.Equal(t, encryptedKey.Crypto.Cipher, "test")

	require.Equal(t, encryptedKey.Crypto.CipherText, "test")

	wantAccount, encryptedKey, err = accountCache.find(cachetestAccounts[1])
	require.NoError(t, err)

	require.Equal(t, listOfEncryptedKeys[1], encryptedKey)

	require.Equal(t, wantAccount.Address.String(), encryptedKey.Address)
}
