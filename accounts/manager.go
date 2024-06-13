package accounts

import (
	"reflect"
	"sync"

	"github.com/0xPolygon/polygon-edge/accounts/event"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
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

type Manager struct {
	backends    map[reflect.Type][]Backend
	updates     chan event.Event
	newBackends chan event.Event
	wallets     []Wallet

	quit chan chan error

	logger hclog.Logger

	eventHandler *event.EventHandler

	term chan struct{}
	lock sync.RWMutex
}

func NewManager(logger hclog.Logger, backends ...Backend) *Manager {
	var wallets []Wallet

	for _, backend := range backends {
		wallets = merge(wallets, backend.Wallets()...)
	}

	updates := make(chan event.Event, managerSubBufferSize)

	newBackends := make(chan event.Event)

	eventHandler := event.NewEventHandler()

	for _, backend := range backends {
		backend.Subscribe(eventHandler)
	}

	am := &Manager{
		backends:     make(map[reflect.Type][]Backend),
		updates:      updates,
		newBackends:  newBackends,
		wallets:      wallets,
		quit:         make(chan chan error),
		term:         make(chan struct{}),
		eventHandler: eventHandler,
	}

	eventHandler.Subscribe(WalletEventKey, am.updates)

	for _, backend := range backends {
		kind := reflect.TypeOf(backend)
		backend.Subscribe(am.eventHandler)
		am.backends[kind] = append(am.backends[kind], backend)
	}

	go am.update()

	return am
}

func (am *Manager) Close() error {
	for _, w := range am.wallets {
		w.Close()
	}

	errc := make(chan error)
	am.quit <- errc

	return <-errc
}

func (am *Manager) AddBackend(backend Backend) {
	done := make(chan struct{})

	am.newBackends <- newBackendEvent{backend, done}

	<-done
}

func (am *Manager) update() {
	defer func() {
		am.lock.Lock()

		am.eventHandler.Unsubscribe(WalletEventKey, am.updates)

		am.lock.Unlock()
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
				backend.Subscribe(am.eventHandler)
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

func (am *Manager) Backends(kind reflect.Type) []Backend {
	am.lock.RLock()
	defer am.lock.RUnlock()

	return am.backends[kind]
}

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

func (am *Manager) Subscribe(eventHandler *event.EventHandler) {
	am.eventHandler = eventHandler
}

func merge(slice []Wallet, wallets ...Wallet) []Wallet {
	for _, wallet := range wallets {
		slice = append(slice, wallet)
	}

	return slice
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
