package accounts

import (
	"reflect"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts/event"
	"github.com/0xPolygon/polygon-edge/blockchain"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type keystoreMock struct {
	mock.Mock
}

func (m *keystoreMock) Wallets() []Wallet {
	args := m.Called()

	return args.Get(0).([]Wallet)
}

func (m *keystoreMock) SetEventHandler(eventHandler *event.EventHandler) {

}

func (m *keystoreMock) SetManager(manager AccountManager) {

}

type keystoreWalletMock struct {
	mock.Mock
}

func (m *keystoreWalletMock) Status() (string, error) {
	args := m.Called()

	return args.String(0), args.Error(1)
}

func (m *keystoreWalletMock) Open(passphrase string) error {
	args := m.Called(passphrase)

	return args.Error(0)
}

func (m *keystoreWalletMock) Close() error {
	args := m.Called()

	return args.Error(0)
}

func (m *keystoreWalletMock) Accounts() []Account {
	args := m.Called()

	return args.Get(0).([]Account)
}

func (m *keystoreWalletMock) Contains(account Account) bool {
	args := m.Called(account)

	return args.Bool(0)
}

func (m *keystoreWalletMock) SignData(account Account, mimeType string, data []byte) ([]byte, error) {
	args := m.Called(account, mimeType, data)

	return args.Get(0).([]byte), args.Error(1)
}

func (m *keystoreWalletMock) SignDataWithPassphrase(account Account,
	passphrase, mimeType string,
	data []byte) ([]byte, error) {
	args := m.Called(account, passphrase, mimeType, data)

	return args.Get(0).([]byte), args.Error(1)
}

func (m *keystoreWalletMock) SignText(account Account, text []byte) ([]byte, error) {
	args := m.Called(account, text)

	return args.Get(0).([]byte), args.Error(1)
}

func (m *keystoreWalletMock) SignTextWithPassphrase(account Account, passphrase string, hash []byte) ([]byte, error) {
	args := m.Called(account, passphrase, hash)

	return args.Get(0).([]byte), args.Error(1)
}

func (m *keystoreWalletMock) SignTx(account Account, tx *types.Transaction) (*types.Transaction, error) {
	args := m.Called(account, tx)

	return args.Get(0).(*types.Transaction), args.Error(1)
}

// SignTxWithPassphrase is identical to SignTx, but also takes a password
func (m *keystoreWalletMock) SignTxWithPassphrase(account Account,
	passphrase string,
	tx *types.Transaction) (*types.Transaction, error) {
	args := m.Called(account, passphrase, tx)

	return args.Get(0).(*types.Transaction), args.Error(1)
}

var keystoreMockType = reflect.TypeOf(&keystoreMock{})

// TestNewManager tests the creation of a new Manager instance
func TestNewManager(t *testing.T) {
	ksMock := new(keystoreMock)
	ksMock.On("Wallets").Return([]Wallet{&keystoreWalletMock{}}).Once()

	manager := NewManager(blockchain.NewTestBlockchain(t, nil), ksMock)

	require.NotNil(t, manager)
	require.Len(t, manager.Wallets(), 1)
	require.Len(t, manager.WalletManagers(keystoreMockType), 1)
}

// TestAddWalletManager tests adding a new wallet manager
func TestAddWalletManager(t *testing.T) {
	manager := NewManager(blockchain.NewTestBlockchain(t, nil))

	ksMock := new(keystoreMock)
	ksMock.On("Wallets").Return([]Wallet{&keystoreWalletMock{}}).Once()

	manager.AddWalletManager(ksMock)
	require.Len(t, manager.Wallets(), 1)
	require.Len(t, manager.WalletManagers(keystoreMockType), 1)
}

func TestClose(t *testing.T) {
	ksMock := new(keystoreMock)

	keystoreWallet := new(keystoreWalletMock)
	ksMock.On("Wallets").Return([]Wallet{keystoreWallet})
	keystoreWallet.On("Close").Return(nil)

	manager := NewManager(blockchain.NewTestBlockchain(t, nil), ksMock)

	require.NotNil(t, manager)
	require.NoError(t, manager.Close())
}

func TestFind(t *testing.T) {
	account := Account{Address: types.ZeroAddress}

	ksMock := new(keystoreMock)

	keystoreWallet := new(keystoreWalletMock)

	ksMock.On("Wallets").Return([]Wallet{keystoreWallet})
	keystoreWallet.On("Contains", account).Return(true).Once()
	keystoreWallet.On("Contains", account).Return(false).Once()

	manager := NewManager(blockchain.NewTestBlockchain(t, nil), ksMock)
	wallet, err := manager.Find(account)
	require.NoError(t, err)
	require.NotNil(t, wallet)

	wallet, err = manager.Find(account)
	require.Error(t, err)
	require.Nil(t, wallet)
}

func TestAccounts(t *testing.T) {
	account := Account{Address: types.ZeroAddress}

	ksMock := new(keystoreMock)

	ksWalletMock := new(keystoreWalletMock)

	ksMock.On("Wallets").Return([]Wallet{ksWalletMock})
	ksWalletMock.On("Accounts").Return([]Account{account})

	manager := NewManager(blockchain.NewTestBlockchain(t, nil), ksMock)
	accounts := manager.Accounts()
	require.Len(t, accounts, 1)
	require.Equal(t, types.ZeroAddress, accounts[0])
}

func TestGetSigner(t *testing.T) {
	ksMock := new(keystoreMock)

	ksWalletMock := new(keystoreWalletMock)

	ksMock.On("Wallets").Return([]Wallet{ksWalletMock})

	manager := NewManager(blockchain.NewTestBlockchain(t, nil), ksMock)

	require.NotNil(t, manager.GetSigner())
}

func TestArrivedDrop(t *testing.T) {
	ksMock := new(keystoreMock)

	ksWalletMock := new(keystoreWalletMock)
	ksWalletMock.On("Accounts").Return([]Account{{Address: types.StringToAddress("0x1")}})

	ksMock.On("Wallets").Return([]Wallet{ksWalletMock})

	manager := NewManager(blockchain.NewTestBlockchain(t, nil), ksMock)

	ksWalletMockArrivedDropped := new(keystoreWalletMock)
	ksWalletMockArrivedDropped.On("Accounts").Return([]Account{{Address: types.StringToAddress("0x2")}})

	manager.eventHandler.Publish(WalletEventKey, WalletEvent{Wallet: ksWalletMockArrivedDropped, Kind: WalletArrived})

	time.Sleep(2 * time.Second)

	require.Len(t, manager.Wallets(), 2)

	manager.eventHandler.Publish(WalletEventKey, WalletEvent{Wallet: ksWalletMockArrivedDropped, Kind: WalletDropped})

	time.Sleep(2 * time.Second)

	require.Len(t, manager.Wallets(), 1)
}
