package jsonrpc

import (
	"errors"
	"fmt"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
)

type Personal struct {
	accManager accounts.AccountManager
}

func NewPersonal(manager accounts.AccountManager) *Personal {
	return &Personal{accManager: manager}
}

func (p *Personal) ListAccounts() ([]types.Address, Error) {
	return p.accManager.Accounts(), nil
}

func (p *Personal) NewAccount(password string) (types.Address, error) {
	ks, err := getKeystore(p.accManager)
	if err != nil {
		return types.ZeroAddress, err
	}

	acc, err := ks.NewAccount(password)
	if err != nil {
		return types.ZeroAddress, fmt.Errorf("can't create new account")
	}

	return acc.Address, nil
}

func (p *Personal) UpdatePassphrase(addr types.Address, oldPassphrase, newPassphrase string) (bool, error) {
	ks, err := getKeystore(p.accManager)
	if err != nil {
		return false, err
	}

	err = ks.Update(accounts.Account{Address: addr}, oldPassphrase, newPassphrase)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (p *Personal) ImportRawKey(privKey string, password string) (types.Address, error) {
	key, err := crypto.HexToECDSA(privKey)
	if err != nil {
		return types.ZeroAddress, err
	}

	ks, err := getKeystore(p.accManager)
	if err != nil {
		return types.ZeroAddress, err
	}

	acc, err := ks.ImportECDSA(key, password)

	return acc.Address, err
}

func (p *Personal) UnlockAccount(addr types.Address, password string, duration uint64) (bool, error) {
	const max = 5 * time.Minute

	var d time.Duration

	if duration == 0 || time.Duration(duration)*time.Second > max {
		d = max
	} else {
		d = time.Duration(duration) * time.Second
	}

	ks, err := getKeystore(p.accManager)
	if err != nil {
		return false, err
	}

	err = ks.TimedUnlock(accounts.Account{Address: addr}, password, d)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (p *Personal) LockAccount(addr types.Address) (bool, error) {
	ks, err := getKeystore(p.accManager)
	if err != nil {
		return false, err
	}

	if err := ks.Lock(addr); err != nil {
		return false, err
	}

	return true, nil
}

func (p *Personal) Ecrecover(data, sig []byte) (types.Address, error) {
	addressRaw, err := crypto.Ecrecover(data, sig)
	if err != nil {
		return types.ZeroAddress, err
	}

	return types.BytesToAddress(addressRaw), nil
}

func getKeystore(am accounts.AccountManager) (*keystore.KeyStore, error) {
	if ks := am.WalletManagers(keystore.KeyStoreType); len(ks) > 0 {
		return ks[0].(*keystore.KeyStore), nil //nolint:forcetypeassert
	}

	return nil, errors.New("local keystore not used")
}
