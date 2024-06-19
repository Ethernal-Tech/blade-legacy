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

const KeyStoreScheme = "keystore"

var (
	cachetestDir, _   = filepath.Abs(filepath.Join("testdata", KeyStoreScheme))
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

	cache := newAccountStore(cachetestDir, hclog.NewNullLogger())
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

	cache := newAccountStore(tDir, hclog.NewNullLogger())

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
		require.NoError(t, cache.add(a, encryptedKey{}))
	}
	// Add some of them twice to check that they don't get reinserted.
	require.Error(t, cache.add(accs[0], encryptedKey{}))
	require.Error(t, cache.add(accs[2], encryptedKey{}))

	for _, a := range accs {
		require.True(t, cache.hasAddress(a.Address))
	}

	// Expected to return false because this address is not contained in cache
	require.False(t, cache.hasAddress(types.StringToAddress("fd9bd350f08ee3c0c19b85a8e16114a11a60aa4e")))

	// Delete a few keys from the cache.
	for i := 0; i < len(accs); i += 2 {
		require.NoError(t, cache.delete(accs[i]))
	}

	require.NoError(t, cache.delete(accounts.Account{Address: types.StringToAddress("fd9bd350f08ee3c0c19b85a8e16114a11a60aa4e")}))

	// accounts that stay in account_cache, should be true
	wantAccountsAfterDelete := []accounts.Account{
		accs[1],
		accs[3],
		accs[5],
	}

	// deleted accounts should be false after delete
	deletedAccounts := []accounts.Account{
		accs[0],
		accs[2],
		accs[4],
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

	cache := newAccountStore(dir, hclog.NewNullLogger())

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
		require.NoError(t, cache.add(acc, encryptedKey{}))
	}

	require.Error(t, cache.add(matchAccount, encryptedKey{}))

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
func TestCacheUpdate(t *testing.T) {
	t.Parallel()

	keyDir := t.TempDir()

	accountCache := newAccountStore(keyDir, hclog.NewNullLogger())

	list := accountCache.accounts()
	if len(list) > 0 {
		t.Error("initial account list not empty:", list)
	}

	account := cachetestAccounts[0]

	require.NoError(t, accountCache.add(account, encryptedKey{Address: account.Address.String(), Crypto: Crypto{Cipher: "test", CipherText: "test"}}))

	require.NoError(t, accountCache.update(account, encryptedKey{Address: cachetestAccounts[0].Address.String(), Crypto: Crypto{Cipher: "testUpdate", CipherText: "testUpdate"}}))

	wantAccount, encryptedKey, err := accountCache.find(account)
	require.NoError(t, err)

	require.Equal(t, wantAccount.Address.String(), encryptedKey.Address)

	require.Equal(t, encryptedKey.Crypto.Cipher, "testUpdate")

	require.Equal(t, encryptedKey.Crypto.CipherText, "testUpdate")
}
