package keystore

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/event"
	"github.com/stretchr/testify/require"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
)

var testSigData = make([]byte, 32)

const pass = "foo"

func TestKeyStore(t *testing.T) {
	t.Parallel()

	_, ks := tmpKeyStore(t)

	a, err := ks.NewAccount(pass)
	if err != nil {
		t.Fatal(err)
	}

	if !ks.HasAddress(a.Address) {
		t.Errorf("HasAccount(%x) should've returned true", a.Address)
	}

	if err := ks.Update(a, pass, "bar"); err != nil {
		t.Errorf("Update error: %v", err)
	}

	if err := ks.Delete(a, "bar"); err != nil {
		t.Errorf("Delete error: %v", err)
	}

	if ks.HasAddress(a.Address) {
		t.Errorf("HasAccount(%x) should've returned true after Delete", a.Address)
	}
}

func TestSign(t *testing.T) {
	t.Parallel()

	_, ks := tmpKeyStore(t)

	pass := "" // not used but required by API

	a1, err := ks.NewAccount(pass)
	if err != nil {
		t.Fatal(err)
	}

	if err := ks.Unlock(a1, pass); err != nil {
		t.Fatal(err)
	}

	if _, err := ks.SignHash(accounts.Account{Address: a1.Address}, testSigData); err != nil {
		t.Fatal(err)
	}
}

func TestSignWithPassphrase(t *testing.T) {
	t.Parallel()
	_, ks := tmpKeyStore(t)

	pass := "passwd"

	acc, err := ks.NewAccount(pass)
	if err != nil {
		t.Fatal(err)
	}

	if _, unlocked := ks.unlocked[acc.Address]; unlocked {
		t.Fatal("expected account to be locked")
	}

	_, err = ks.SignHashWithPassphrase(acc, pass, testSigData)
	if err != nil {
		t.Fatal(err)
	}

	if _, unlocked := ks.unlocked[acc.Address]; unlocked {
		t.Fatal("expected account to be locked")
	}

	if _, err = ks.SignHashWithPassphrase(acc, "invalid passwd", testSigData); err == nil {
		t.Fatal("expected SignHashWithPassphrase to fail with invalid password")
	}
}

func TestTimedUnlock(t *testing.T) {
	t.Parallel()
	_, ks := tmpKeyStore(t)

	a1, err := ks.NewAccount(pass)
	if err != nil {
		t.Fatal(err)
	}

	// Signing without passphrase fails because account is locked
	_, err = ks.SignHash(accounts.Account{Address: a1.Address}, testSigData)
	if !errors.Is(err, ErrLocked) {
		t.Fatal("Signing should've failed with ErrLocked before unlocking, got ", err)
	}

	// Signing with passphrase works
	if err = ks.TimedUnlock(a1, pass, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Signing without passphrase works because account is temp unlocked
	_, err = ks.SignHash(accounts.Account{Address: a1.Address}, testSigData)
	if err != nil {
		t.Fatal("Signing shouldn't return an error after unlocking, got ", err)
	}

	// Signing fails again after automatic locking
	time.Sleep(250 * time.Millisecond)

	_, err = ks.SignHash(accounts.Account{Address: a1.Address}, testSigData)
	if !errors.Is(err, ErrLocked) {
		t.Fatal("Signing should've failed with ErrLocked timeout expired, got ", err)
	}
}

func TestOverrideUnlock(t *testing.T) {
	t.Parallel()
	_, ks := tmpKeyStore(t)

	a1, err := ks.NewAccount(pass)
	if err != nil {
		t.Fatal(err)
	}

	// Unlock indefinitely.
	if err = ks.TimedUnlock(a1, pass, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	// Signing without passphrase works because account is temp unlocked
	_, err = ks.SignHash(accounts.Account{Address: a1.Address}, testSigData)
	if err != nil {
		t.Fatal("Signing shouldn't return an error after unlocking, got ", err)
	}

	// reset unlock to a shorter period, invalidates the previous unlock
	if err = ks.TimedUnlock(a1, pass, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Signing without passphrase still works because account is temp unlocked
	_, err = ks.SignHash(accounts.Account{Address: a1.Address}, testSigData)
	if err != nil {
		t.Fatal("Signing shouldn't return an error after unlocking, got ", err)
	}

	// Signing fails again after automatic locking
	time.Sleep(250 * time.Millisecond)

	_, err = ks.SignHash(accounts.Account{Address: a1.Address}, testSigData)
	if !errors.Is(err, ErrLocked) {
		t.Fatal("Signing should've failed with ErrLocked timeout expired, got ", err)
	}
}

// This test should fail under -race if signing races the expiration goroutine.
func TestSignRace(t *testing.T) {
	t.Parallel()
	_, ks := tmpKeyStore(t)

	pass := ""

	// Create a test account.
	a1, err := ks.NewAccount(pass)
	if err != nil {
		t.Fatal("could not create the test account", err)
	}

	if err := ks.TimedUnlock(a1, pass, 15*time.Millisecond); err != nil {
		t.Fatal("could not unlock the test account", err)
	}

	end := time.Now().UTC().Add(500 * time.Millisecond)

	for time.Now().UTC().Before(end) {
		if _, err := ks.SignHash(accounts.Account{Address: a1.Address}, testSigData); errors.Is(err, ErrLocked) {
			return
		} else if err != nil {
			t.Errorf("Sign error: %v", err)

			return
		}

		time.Sleep(1 * time.Millisecond)
	}

	t.Errorf("Account did not lock within the timeout")
}

type walletEvent struct {
	accounts.WalletEvent
	a accounts.Account
}

// Tests that wallet notifications and correctly fired when accounts are added
// or deleted from the keystore.
func TestWalletNotifications(t *testing.T) {
	t.Parallel()
	_, ks := tmpKeyStore(t)
	eventHandler := event.NewEventHandler()
	ks.SetEventHandler(eventHandler)

	// Subscribe to the wallet feed and collect events.
	var (
		events  []walletEvent
		updates = make(chan event.Event)
		end     = make(chan interface{})
	)

	ks.eventHandler.Subscribe(accounts.WalletEventKey, updates)

	defer eventHandler.Unsubscribe(accounts.WalletEventKey, updates)

	go func() {
		for {
			select {
			case ev := <-updates:
				events = append(events, walletEvent{ev.(accounts.WalletEvent), ev.(accounts.WalletEvent).Wallet.Accounts()[0]})
			case <-end:
				eventHandler.Unsubscribe(accounts.WalletEventKey, updates)

				return
			}
		}
	}()

	// Randomly add and remove accounts.
	var (
		live       = make(map[types.Address]accounts.Account)
		wantEvents []walletEvent
	)

	for i := 0; i < 1024; i++ {
		if create := len(live) == 0 || rand.Int()%4 > 0; create {
			// Add a new account and ensure wallet notifications arrives
			account, err := ks.NewAccount("")
			if err != nil {
				t.Fatalf("failed to create test account: %v", err)
			}

			live[account.Address] = account
			wantEvents = append(wantEvents, walletEvent{accounts.WalletEvent{Kind: accounts.WalletArrived}, account})
		} else {
			// Delete a random account.
			var account accounts.Account

			for _, a := range live {
				account = a

				break
			}

			if err := ks.Delete(account, ""); err != nil {
				t.Fatalf("failed to delete test account: %v", err)
			}

			delete(live, account.Address)
			wantEvents = append(wantEvents, walletEvent{accounts.WalletEvent{Kind: accounts.WalletDropped}, account})
		}
	}

	end <- new(interface{})

	for ev := range updates {
		events = append(events, walletEvent{ev.(accounts.WalletEvent), ev.(accounts.WalletEvent).Wallet.Accounts()[0]})
	}

	checkAccounts(t, live, ks.Wallets())
	checkEvents(t, wantEvents, events)

	// Shut down the event collector and check events.
	eventHandler.Unsubscribe(accounts.WalletEventKey, updates)
}

// TestImportECDSA tests the import functionality of a keystore.
func TestImportECDSA(t *testing.T) {
	t.Parallel()

	_, ks := tmpKeyStore(t)

	key, err := crypto.GenerateECDSAPrivateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", key)
	}

	if _, err = ks.ImportECDSA(key, "old"); err != nil {
		t.Errorf("importing failed: %v", err)
	}

	if _, err = ks.ImportECDSA(key, "old"); err == nil {
		t.Errorf("importing same key twice succeeded")
	}

	if _, err = ks.ImportECDSA(key, "new"); err == nil {
		t.Errorf("importing same key twice succeeded")
	}
}

// checkAccounts checks that all known live accounts are present in the wallet list.
func checkAccounts(t *testing.T, live map[types.Address]accounts.Account, wallets []accounts.Wallet) {
	t.Helper()

	if len(live) != len(wallets) {
		t.Errorf("wallet list doesn't match required accounts: have %d, want %d", len(wallets), len(live))

		return
	}

	liveList := make([]accounts.Account, 0, len(live))

	for _, account := range live {
		liveList = append(liveList, account)
	}

	for j, wallet := range wallets {
		if accs := wallet.Accounts(); len(accs) != 1 {
			t.Errorf("wallet %d: contains invalid number of accounts: have %d, want 1", j, len(accs))
		}

		isFind := false

		for _, liveWallet := range liveList {
			if liveWallet == wallet.Accounts()[0] {
				isFind = true

				break
			}
		}

		if !isFind {
			t.Errorf("wallet %d: account mismatch: have %v, want %v", j, wallet, liveList[j])
		}
	}
}

// checkEvents checks that all events in 'want' are present in 'have'. Events may be present multiple times.
func checkEvents(t *testing.T, want []walletEvent, have []walletEvent) {
	t.Helper()

	for _, wantEv := range want {
		nmatch := 0

		for ; len(have) > 0; nmatch++ {
			if have[0].Kind != wantEv.Kind || have[0].a != wantEv.a {
				break
			}

			have = have[1:]
		}

		if nmatch == 0 {
			t.Fatalf("can't find event with Kind=%v for %x", wantEv.Kind, wantEv.a.Address)
		}
	}
}

func tmpKeyStore(t *testing.T) (string, *KeyStore) {
	t.Helper()

	d := t.TempDir()

	ks, err := NewKeyStore(d, veryLightScryptN, veryLightScryptP, hclog.NewNullLogger())
	require.NoError(t, err)

	ks.eventHandler = event.NewEventHandler()

	return d, ks
}
