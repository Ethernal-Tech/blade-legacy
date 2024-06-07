package external

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/event"
	jsonTypes "github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo/jsonrpc"
)

type ExternalBackend struct {
	signers []accounts.Wallet
}

func (eb *ExternalBackend) Wallets() []accounts.Wallet {
	return eb.signers
}

func NewExternalBackend(endpoint string) (*ExternalBackend, error) {
	signer, err := NewExternalSigner(endpoint)
	if err != nil {
		return nil, err
	}

	return &ExternalBackend{
		signers: []accounts.Wallet{signer},
	}, nil
}

func (eb *ExternalBackend) Subscribe(sink chan<- accounts.WalletEvent) event.Subscription {
	return event.NewSubscription(func(quit <-chan struct{}) error {
		<-quit

		return nil
	})
}

// ExternalSigner provides an API to interact with an external signer (clef)
// It proxies request to the external signer while forwarding relevant
// request headers
type ExternalSigner struct {
	client   *jsonrpc.Client
	endpoint string
	status   string
	cacheMu  sync.RWMutex
	cache    []accounts.Account
}

func NewExternalSigner(endpoint string) (*ExternalSigner, error) {
	client, err := jsonrpc.NewClient(endpoint)
	if err != nil {
		return nil, err
	}

	extsigner := &ExternalSigner{
		client:   client,
		endpoint: endpoint,
	}

	// Check if reachable
	version, err := extsigner.pingVersion()
	if err != nil {
		return nil, err
	}

	extsigner.status = fmt.Sprintf("ok [version=%v]", version)

	return extsigner, nil
}

func (api *ExternalSigner) URL() accounts.URL {
	return accounts.URL{
		Scheme: "extapi",
		Path:   api.endpoint,
	}
}

func (api *ExternalSigner) Status() (string, error) {
	return api.status, nil
}

func (api *ExternalSigner) Open(passphrase string) error {
	return errors.New("operation not supported on external signers")
}

func (api *ExternalSigner) Close() error {
	return errors.New("operation not supported on external signers")
}

func (api *ExternalSigner) Accounts() []accounts.Account {
	var accnts []accounts.Account //nolint:prealloc

	res, err := api.listAccounts()
	if err != nil {
		return accnts
	}

	for _, addr := range res {
		accnts = append(accnts, accounts.Account{
			URL: accounts.URL{
				Scheme: "extapi",
				Path:   api.endpoint,
			},
			Address: addr,
		})
	}

	api.cacheMu.Lock()
	api.cache = accnts
	api.cacheMu.Unlock()

	return accnts
}

func (api *ExternalSigner) Contains(account accounts.Account) bool {
	api.cacheMu.RLock()
	defer api.cacheMu.RUnlock()

	if api.cache == nil {
		// If we haven't already fetched the accounts, it's time to do so now
		api.cacheMu.RUnlock()
		api.Accounts()
		api.cacheMu.RLock()
	}

	for _, a := range api.cache {
		if a.Address == account.Address && (account.URL == (accounts.URL{}) || account.URL == api.URL()) {
			return true
		}
	}

	return false
}

// SignData signs keccak256(data). The mimetype parameter describes the type of data being signed
func (api *ExternalSigner) SignData(account accounts.Account, mimeType string, data []byte) ([]byte, error) {
	var (
		hexData     []byte
		res         []byte
		signAddress = types.NewMixedcaseAddress(account.Address)
	)

	hex.Encode(hexData, data)

	if err := api.client.Call("account_signData", &res,
		mimeType,
		&signAddress, // Need to use the pointer here, because of how MarshalJSON is defined
		hexData); err != nil {
		return nil, err
	}

	// If V is on 27/28-form, convert to 0/1 for Clique
	if mimeType == accounts.MimetypeClique && (res[64] == 27 || res[64] == 28) {
		res[64] -= 27 // Transform V from 27/28 to 0/1 for Clique use
	}

	return res, nil
}

func (api *ExternalSigner) SignText(account accounts.Account, text []byte) ([]byte, error) {
	var (
		signature   []byte
		signAddress = types.NewMixedcaseAddress(account.Address)
		textHex     []byte
	)

	hex.Encode(textHex, text)

	if err := api.client.Call("account_signData",
		&signature,
		accounts.MimetypeTextPlain,
		&signAddress, // Need to use the pointer here, because of how MarshalJSON is defined
		textHex); err != nil {
		return nil, err
	}

	if signature[64] == 27 || signature[64] == 28 {
		// If clef is used as a backend, it may already have transformed
		// the signature to ethereum-type signature.
		signature[64] -= 27 // Transform V from Ethereum-legacy to 0/1
	}

	return signature, nil
}

// signTransactionResult represents the signinig result returned by clef.
type signTransactionResult struct {
	Raw []byte             `json:"raw"`
	Tx  *types.Transaction `json:"tx"`
}

// SignTx sends the transaction to the external signer.
// If chainID is nil, or tx.ChainID is zero, the chain ID will be assigned
// by the external signer. For non-legacy transactions, the chain ID of the
// transaction overrides the chainID parameter.
func (api *ExternalSigner) SignTx(account accounts.Account,
	tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	var to *types.MixedcaseAddress

	if tx.To() != nil {
		t := types.NewMixedcaseAddress(*tx.To())
		to = t
	}

	args := &jsonTypes.SendTxnArgs{
		Input: jsonTypes.ArgBytesPtr(tx.Input()),
		Nonce: jsonTypes.ArgUintPtr(tx.Nonce()),
		Value: jsonTypes.ArgBytesPtr(tx.Value().Bytes()),
		Gas:   jsonTypes.ArgUintPtr(tx.Gas()),
		To:    to,
		From:  types.NewMixedcaseAddress(account.Address),
	}

	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType, types.StateTxType:
		args.GasPrice = jsonTypes.ArgBytesPtr(tx.GasPrice().Bytes())
	case types.DynamicFeeTxType:
		args.GasTipCap = jsonTypes.ArgBytesPtr(tx.GasFeeCap().Bytes())
		args.GasFeeCap = jsonTypes.ArgBytesPtr(tx.GasTipCap().Bytes())
	default:
		return nil, fmt.Errorf("unsupported tx type %d", tx.Type())
	}

	// We should request the default chain id that we're operating with
	// (the chain we're executing on)
	if chainID != nil && chainID.Sign() != 0 {
		args.ChainID = jsonTypes.ArgUintPtr(chainID.Uint64())
	}

	if tx.Type() == types.DynamicFeeTxType {
		if tx.ChainID().Sign() != 0 {
			args.ChainID = jsonTypes.ArgUintPtr(tx.ChainID().Uint64())
		}
	} else if tx.Type() == types.AccessListTxType {
		// However, if the user asked for a particular chain id, then we should
		// use that instead.
		if tx.ChainID().Sign() != 0 {
			args.ChainID = jsonTypes.ArgUintPtr(tx.ChainID().Uint64())
		}

		accessList := tx.AccessList()
		args.AccessList = &accessList
	}

	var res signTransactionResult

	if err := api.client.Call("account_signTransaction", args, &res); err != nil {
		return nil, err
	}

	return res.Tx, nil
}

func (api *ExternalSigner) SignTextWithPassphrase(account accounts.Account,
	passphrase string, text []byte) ([]byte, error) {
	return []byte{}, errors.New("password-operations not supported on external signers")
}

func (api *ExternalSigner) SignTxWithPassphrase(account accounts.Account,
	passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	return nil, errors.New("password-operations not supported on external signers")
}

func (api *ExternalSigner) SignDataWithPassphrase(account accounts.Account,
	passphrase, mimeType string, data []byte) ([]byte, error) {
	return nil, errors.New("password-operations not supported on external signers")
}

func (api *ExternalSigner) listAccounts() ([]types.Address, error) {
	var res []types.Address

	if err := api.client.Call("account_list", nil, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (api *ExternalSigner) pingVersion() (string, error) {
	var v string

	if err := api.client.Call("account_version", nil, &v); err != nil {
		return "", err
	}

	return v, nil
}
