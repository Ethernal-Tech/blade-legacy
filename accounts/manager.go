package accounts

import (
	"reflect"
	"sync"

	"github.com/0xPolygon/polygon-edge/accounts/event"
	"github.com/0xPolygon/polygon-edge/blockchain"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	managerSubBufferSize = 50

	WalletEventKey = "walletEvent"
)

type newBackendEvent struct {
	backend Backend

	processed chan struct{}
}

func (newBackendEvent) Type() event.EventType {
	return event.NewBackendType
}

// Manager is an overarching account manager that can communicate with various
// backends for signing transactions.

type Manager struct {
	backends    map[reflect.Type][]Backend
	updates     chan event.Event
	newBackends chan event.Event
	wallets     []Wallet
	blockchain  *blockchain.Blockchain

	quit chan chan error

	eventHandler *event.EventHandler

	term chan struct{}
	lock sync.RWMutex
}

// Creates new instance of manager
func NewManager(blockchain *blockchain.Blockchain, backends ...Backend) *Manager {
	var wallets []Wallet

	for _, backend := range backends {
		wallets = merge(wallets, backend.Wallets()...)
	}

	updates := make(chan event.Event, managerSubBufferSize)
	newBackends := make(chan event.Event)
	eventHandler := event.NewEventHandler()

	for _, backend := range backends {
		backend.SetEventHandler(eventHandler)
	}

	am := &Manager{
		backends:     make(map[reflect.Type][]Backend),
		updates:      updates,
		newBackends:  newBackends,
		wallets:      wallets,
		quit:         make(chan chan error),
		term:         make(chan struct{}),
		eventHandler: eventHandler,
		blockchain:   blockchain,
	}

	eventHandler.Subscribe(WalletEventKey, am.updates)

	for _, backend := range backends {
		kind := reflect.TypeOf(backend)

		backend.SetEventHandler(am.eventHandler)
		backend.SetManager(am)
		am.backends[kind] = append(am.backends[kind], backend)
	}

	go am.update()

	return am
}

// Close stop updater in manager
func (am *Manager) Close() error {
	am.lock.RLock()
	defer am.lock.RUnlock()

	for _, w := range am.wallets {
		w.Close()
	}

	errc := make(chan error)
	am.quit <- errc

	return <-errc
}

// Adds backend to list of backends
func (am *Manager) AddBackend(backend Backend) {
	done := make(chan struct{})

	am.newBackends <- newBackendEvent{backend, done}

	<-done
}

func (am *Manager) update() {
	defer func() {
		am.eventHandler.Unsubscribe(WalletEventKey, am.updates)
	}()

	for {
		select {
		case eventChan := <-am.updates:
			am.lock.Lock()

			if eventChan.Type() == event.WalletEventType {
				walletEvent := eventChan.(WalletEvent) //nolint:forcetypeassert
				switch walletEvent.Kind {
				case WalletArrived:
					am.wallets = merge(am.wallets, walletEvent.Wallet)
				case WalletDropped:
					am.wallets = drop(am.wallets, walletEvent.Wallet)
				}
			}

			am.lock.Unlock()
		case backendEventChan := <-am.newBackends:
			am.lock.Lock()

			if backendEventChan.Type() == event.NewBackendType {
				bckEvent := backendEventChan.(newBackendEvent) //nolint:forcetypeassert
				backend := bckEvent.backend
				am.wallets = merge(am.wallets, backend.Wallets()...)
				backend.SetEventHandler(am.eventHandler)
				kind := reflect.TypeOf(backend)
				am.backends[kind] = append(am.backends[kind], backend)
				am.lock.Unlock()
				close(bckEvent.processed)
			}

			am.lock.Unlock()
		case errc := <-am.quit:
			errc <- nil

			close(am.term)

			return
		}
	}
}

// Return specific type of backend
func (am *Manager) Backends(kind reflect.Type) []Backend {
	am.lock.RLock()
	defer am.lock.RUnlock()

	return am.backends[kind]
}

// Return list of all wallets
func (am *Manager) Wallets() []Wallet {
	am.lock.RLock()
	defer am.lock.RUnlock()

	return am.walletsNoLock()
}

func (am *Manager) walletsNoLock() []Wallet {
	cpy := make([]Wallet, len(am.wallets))
	copy(cpy, am.wallets)

	return cpy
}

// Return all accounts
func (am *Manager) Accounts() []types.Address {
	am.lock.RLock()
	defer am.lock.RUnlock()

	addresses := make([]types.Address, 0)

	for _, wallet := range am.wallets {
		for _, account := range wallet.Accounts() {
			addresses = append(addresses, account.Address)
		}
	}

	return addresses
}

// Checks for active forks at current block number and return signer
func (am *Manager) GetSigner() crypto.TxSigner {
	return crypto.NewSigner(
		am.blockchain.Config().Forks.At(am.blockchain.Header().Number),
		uint64(am.blockchain.Config().ChainID))
}

// Search through wallets and
func (am *Manager) Find(account Account) (Wallet, error) {
	am.lock.RLock()
	defer am.lock.RUnlock()

	for _, wallet := range am.wallets {
		if wallet.Contains(account) {
			return wallet, nil
		}
	}

	return nil, ErrUnknownAccount
}

func merge(slice []Wallet, wallets ...Wallet) []Wallet {
	return append(slice, wallets...)
}

func drop(slice []Wallet, wallet Wallet) []Wallet {
	var droppedSlice []Wallet

	for _, internalWallet := range slice {
		if internalWallet.Accounts()[0].Address != wallet.Accounts()[0].Address {
			droppedSlice = append(droppedSlice, internalWallet)
		}
	}

	return droppedSlice
}
