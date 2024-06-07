package accounts

import (
	"reflect"
	"sort"
	"sync"

	"github.com/0xPolygon/polygon-edge/accounts/event"
	"github.com/0xPolygon/polygon-edge/types"
)

const managerSubBufferSize = 50

type Config struct {
	InsecureUnlockAllowed bool
}

type newBackendEvent struct {
	backend Backend

	processed chan struct{}
}

type Manager struct {
	config      *Config
	backends    map[reflect.Type][]Backend
	updaters    []event.Subscription
	updates     chan WalletEvent
	newBackends chan newBackendEvent
	wallets     []Wallet

	feed event.Feed

	quit chan chan error

	term chan struct{}
	lock sync.RWMutex
}

func NewManager(config *Config, backends ...Backend) *Manager {
	var wallets []Wallet

	for _, backend := range backends {
		wallets = merge(wallets, backend.Wallets()...)
	}

	updates := make(chan WalletEvent, managerSubBufferSize)

	subs := make([]event.Subscription, len(backends))

	for i, backend := range backends {
		subs[i] = backend.Subscribe(updates)
	}

	am := &Manager{
		config:      config,
		backends:    make(map[reflect.Type][]Backend),
		updaters:    subs,
		updates:     updates,
		newBackends: make(chan newBackendEvent),
		wallets:     wallets,
		quit:        make(chan chan error),
		term:        make(chan struct{}),
	}

	for _, backend := range backends {
		kind := reflect.TypeOf(backend)
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

func (am *Manager) Config() *Config {
	return am.config
}

func (am *Manager) AddBackend(backend Backend) {
	done := make(chan struct{})

	am.newBackends <- newBackendEvent{backend, done}

	<-done
}

func (am *Manager) update() {
	defer func() {
		am.lock.Lock()

		for _, sub := range am.updaters {
			sub.Unsubscribe()
		}

		am.updaters = nil
		am.lock.Unlock()
	}()

	for {
		select {
		case event := <-am.updates:
			am.lock.Lock()
			switch event.Kind {
			case WalletArrived:
				am.wallets = merge(am.wallets, event.Wallet)
			case WalletDropped:
				am.wallets = drop(am.wallets, event.Wallet)
			}
			am.lock.Unlock()

			am.feed.Send(event)
		case event := <-am.newBackends:
			am.lock.Lock()

			backend := event.backend
			am.wallets = merge(am.wallets, backend.Wallets()...)
			am.updaters = append(am.updaters, backend.Subscribe(am.updates))
			kind := reflect.TypeOf(backend)
			am.backends[kind] = append(am.backends[kind], backend)
			am.lock.Unlock()
			close(event.processed)
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

func (am *Manager) Wallet(url string) (Wallet, error) {
	am.lock.RLock()
	defer am.lock.RUnlock()

	parsed, err := parseURL(url)
	if err != nil {
		return nil, err
	}

	for _, wallet := range am.walletsNoLock() {
		if wallet.URL() == parsed {
			return wallet, nil
		}
	}

	return nil, ErrUnknownWallet
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

func (am *Manager) Subscribe(sink chan<- WalletEvent) event.Subscription {
	return am.feed.Subscribe(sink)
}

func merge(slice []Wallet, wallets ...Wallet) []Wallet {
	for _, wallet := range wallets {
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(wallet.URL()) >= 0 })
		if n == len(slice) {
			continue
		}

		slice = append(slice[:n], slice[n+1:]...)
	}

	return slice
}

func drop(slice []Wallet, wallets ...Wallet) []Wallet {
	for _, wallet := range wallets {
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(wallet.URL()) >= 0 })

		if n == len(slice) {
			continue
		}

		slice = append(slice[:n], slice[n+1:]...)
	}

	return slice
}
