package keystore

import (
	"errors"
	"math/big"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
)

type keyStoreWallet struct {
	account  accounts.Account
	keyStore *KeyStore
}

func (ksw *keyStoreWallet) URL() accounts.URL {
	return ksw.account.URL
}

func (ksw *keyStoreWallet) Status() (string, error) {
	ksw.keyStore.mu.RLock()
	defer ksw.keyStore.mu.RUnlock()

	if _, ok := ksw.keyStore.unlocked[ksw.account.Address]; ok {
		return "Unlocked", nil
	}

	return "Locked", nil
}

func (ksw *keyStoreWallet) Open(passphrase string) error { return nil }

func (ksw *keyStoreWallet) Close() error { return nil }

func (ksw *keyStoreWallet) Accounts() []accounts.Account {
	return []accounts.Account{ksw.account}
}

func (ksw *keyStoreWallet) Contains(account accounts.Account) bool {
	return account.Address == ksw.account.Address && (account.URL == accounts.URL{} || account.URL == ksw.account.URL)
}

func (ksw *keyStoreWallet) Derive(path accounts.DerivationPath, pin bool) (accounts.Account, error) {
	return accounts.Account{}, accounts.ErrNotSupported
}

func (ksw *keyStoreWallet) SelfDerive(bases []accounts.DerivationPath, state *state.Transition) {}

func (ksw *keyStoreWallet) signHash(account accounts.Account, hash []byte) ([]byte, error) {
	if !ksw.Contains(account) {
		return nil, accounts.ErrUnknownAccount
	}

	return ksw.keyStore.SignHash(account, hash)
}

func (ksw *keyStoreWallet) SignData(account accounts.Account, mimeType string, data []byte) ([]byte, error) {
	return ksw.signHash(account, crypto.Keccak256(data))
}

func (ksw *keyStoreWallet) SignDataWithPassphrase(account accounts.Account, passhphrase, mimeType string, data []byte) ([]byte, error) {
	if !ksw.Contains(account) {
		return nil, accounts.ErrUnknownAccount
	}

	return ksw.keyStore.SignHashWithPassphrase(account, passhphrase, crypto.Keccak256(data))
}

func (ksw *keyStoreWallet) SignText(account accounts.Account, text []byte) ([]byte, error) {
	return ksw.signHash(account, accounts.TextHash(text))
}

func (ksw *keyStoreWallet) SignTextWithPassphrase(account accounts.Account, passphrase string, text []byte) ([]byte, error) {
	if !ksw.Contains(account) {
		return nil, accounts.ErrUnknownAccount
	}

	return ksw.keyStore.SignHashWithPassphrase(account, passphrase, accounts.TextHash(text))
}

func (ksw *keyStoreWallet) SignTx(account accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	if !ksw.Contains(account) {
		return nil, errors.New("unknown account")
	}

	return ksw.keyStore.SignTx(account, tx, chainID)
}

func (ksw *keyStoreWallet) SignTxWithPassphrase(account accounts.Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	if !ksw.Contains(account) {
		return nil, errors.New("unknown account")
	}

	return ksw.keyStore.SignTxWithPassphrase(account, passphrase, tx, chainID)
}
