package jsonrpc

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

type Personal struct {
	accManager *accounts.Manager
}

func (p *Personal) ListAccounts() []types.Address {
	return p.accManager.Accounts()
}

func (p *Personal) NewAccount(password string) (types.Address, error) {
	ks, err := getKeystore(p.accManager)
	if err != nil {
		return types.Address{}, err
	}

	acc, err := ks.NewAccount(password)
	if err != nil {
		return types.Address{}, fmt.Errorf("can't create new account")
	}

	return acc.Address, nil
}

func (p *Personal) ImportRawKey(privKey string, password string) (types.Address, error) {
	key, err := crypto.HexToECDSA(privKey)
	if err != nil {
		return types.Address{}, err
	}

	ks, err := getKeystore(p.accManager)
	if err != nil {
		return types.Address{}, err
	}

	acc, err := ks.ImportECDSA(key, password)

	return acc.Address, err
}

func (p *Personal) UnlockAccount(addr types.Address, password string, duration uint64) (bool, error) {
	const max = uint64(time.Duration(math.MaxInt64) / time.Second)

	var d time.Duration

	switch {
	case duration == 0:
		d = 300 * time.Second
	case duration > max:
		return false, errors.New("unlock duration is too large")
	default:
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

func (s *Personal) LockAccount(addr types.Address) bool {
	if ks, err := getKeystore(s.accManager); err == nil {
		return ks.Lock(addr) == nil
	}

	return false
}

func (p *Personal) Ecrecover(data, sig []byte) (types.Address, error) {
	if len(sig) != crypto.ECDSASignatureLength {
		return types.Address{}, fmt.Errorf("signature must be %d bytes long", crypto.ECDSASignatureLength)
	}

	if sig[64] != 27 && sig[64] != 28 {
		return types.Address{}, errors.New("invalid Ethereum signature V is not 27 or 28")
	}

	sig[64] -= 27

	rpk, _, err := ecdsa.RecoverCompact(sig, data)
	if err != nil {
		return types.Address{}, err
	}

	return crypto.PubKeyToAddress(rpk.ToECDSA()), nil
}

func getKeystore(am *accounts.Manager) (*keystore.KeyStore, error) {
	if ks := am.Backends(keystore.KeyStoreType); len(ks) > 0 {
		return ks[0].(*keystore.KeyStore), nil //nolint:forcetypeassert
	}

	return nil, errors.New("local keystore not used")
}
