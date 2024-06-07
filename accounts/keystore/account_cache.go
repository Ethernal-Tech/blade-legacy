package keystore

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/types"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/hashicorp/go-hclog"
)

func byURL(a, b accounts.Account) int {
	return a.URL.Cmp(b.URL)
}

// KeyStoreScheme is the protocol scheme prefixing account and wallet URLs.
const KeyStoreScheme = "keystore"
const minReloadInterval = 2 * time.Second

// accountCache is a live index of all accounts in the keystore.
type accountCache struct {
	logger   hclog.Logger
	keydir   string
	watcher  *watcher
	mu       sync.Mutex
	all      []accounts.Account
	byAddr   map[types.Address][]accounts.Account
	throttle *time.Timer
	notify   chan struct{}
	fileC    fileCache
}

func newAccountCache(keyDir string, logger hclog.Logger) (*accountCache, chan struct{}) {
	ac := &accountCache{
		logger: logger,
		keydir: keyDir,
		byAddr: make(map[types.Address][]accounts.Account),
		notify: make(chan struct{}, 1),
		fileC:  fileCache{all: mapset.NewThreadUnsafeSet[string]()},
	}
	ac.watcher = newWatcher(ac, logger)

	return ac, ac.notify
}

func (ac *accountCache) accounts() []accounts.Account {
	ac.maybeReload()
	ac.mu.Lock()
	defer ac.mu.Unlock()

	cpy := make([]accounts.Account, len(ac.all))
	copy(cpy, ac.all)

	return cpy
}

func (ac *accountCache) hasAddress(addr types.Address) bool {
	ac.maybeReload()

	ac.mu.Lock()
	defer ac.mu.Unlock()

	return len(ac.byAddr[addr]) > 0
}

func (ac *accountCache) add(newAccount accounts.Account) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	i := sort.Search(len(ac.all), func(i int) bool { return ac.all[i].URL.Cmp(newAccount.URL) >= 0 })
	if i < len(ac.all) && ac.all[i] == newAccount {
		return
	}

	// newAccount is not in the cache.
	ac.all = append(ac.all, accounts.Account{})
	copy(ac.all[i+1:], ac.all[i:])
	ac.all[i] = newAccount
	ac.byAddr[newAccount.Address] = append(ac.byAddr[newAccount.Address], newAccount)
}

// note: removed needs to be unique here (i.e. both File and Address must be set).
func (ac *accountCache) delete(removed accounts.Account) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.all = removeAccount(ac.all, removed)
	if ba := removeAccount(ac.byAddr[removed.Address], removed); len(ba) == 0 {
		delete(ac.byAddr, removed.Address)
	} else {
		ac.byAddr[removed.Address] = ba
	}
}

// deleteByFile removes an account referenced by the given path.
func (ac *accountCache) deleteByFile(path string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	i := sort.Search(len(ac.all), func(i int) bool { return ac.all[i].URL.Path >= path })

	if i < len(ac.all) && ac.all[i].URL.Path == path {
		removed := ac.all[i]
		ac.all = append(ac.all[:i], ac.all[i+1:]...)

		if ba := removeAccount(ac.byAddr[removed.Address], removed); len(ba) == 0 {
			delete(ac.byAddr, removed.Address)
		} else {
			ac.byAddr[removed.Address] = ba
		}
	}
}

// watcherStarted returns true if the watcher loop started running (even if it
// has since also ended).
func (ac *accountCache) watcherStarted() bool {
	ac.mu.Lock()

	defer ac.mu.Unlock()

	return ac.watcher.running || ac.watcher.runEnded
}

func removeAccount(slice []accounts.Account, elem accounts.Account) []accounts.Account {
	for i := range slice {
		if slice[i] == elem {
			return append(slice[:i], slice[i+1:]...)
		}
	}

	return slice
}

// find returns the cached account for address if there is a unique match.
// The exact matching rules are explained by the documentation of accounts.Account.
// Callers must hold ac.mu.
func (ac *accountCache) find(a accounts.Account) (accounts.Account, error) {
	// Limit search to address candidates if possible.
	matches := ac.all
	if (a.Address != types.Address{}) {
		matches = ac.byAddr[a.Address]
	}

	if a.URL.Path != "" {
		// If only the basename is specified, complete the path.
		if !strings.ContainsRune(a.URL.Path, filepath.Separator) {
			a.URL.Path = filepath.Join(ac.keydir, a.URL.Path)
		}

		for i := range matches {
			if matches[i].URL == a.URL {
				return matches[i], nil
			}
		}

		if (a.Address == types.Address{}) {
			return accounts.Account{}, accounts.ErrNoMatch
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return accounts.Account{}, accounts.ErrNoMatch
	default:
		err := &accounts.AmbiguousAddrError{Addr: a.Address, Matches: make([]accounts.Account, len(matches))}
		copy(err.Matches, matches)
		slices.SortFunc(err.Matches, byURL)

		return accounts.Account{}, err
	}
}

func (ac *accountCache) maybeReload() {
	ac.mu.Lock()

	if ac.watcher.running {
		ac.mu.Unlock()

		return // A watcher is running and will keep the cache up-to-date.
	}

	if ac.throttle == nil {
		ac.throttle = time.NewTimer(0)
	} else {
		select {
		case <-ac.throttle.C:
		default:
			ac.mu.Unlock()

			return // The cache was reloaded recently.
		}
	}
	// No watcher running, start it.
	ac.watcher.start()
	ac.throttle.Reset(minReloadInterval)
	ac.mu.Unlock()

	if err := ac.scanAccounts(); err != nil {
		ac.logger.Info("reload", "failed to scan accounts", err)
	}
}

func (ac *accountCache) close() {
	ac.mu.Lock()
	ac.watcher.close()

	if ac.throttle != nil {
		ac.throttle.Stop()
	}

	if ac.notify != nil {
		close(ac.notify)
		ac.notify = nil
	}

	ac.mu.Unlock()
}

// scanAccounts checks if any changes have occurred on the filesystem, and
// updates the account cache accordingly
func (ac *accountCache) scanAccounts() error {
	// Scan the entire folder metadata for file changes
	creates, deletes, updates, err := ac.fileC.scan(ac.keydir)
	if err != nil {
		ac.logger.Debug("Failed to reload keystore contents", "err", err)

		return err
	}

	if creates.Cardinality() == 0 && deletes.Cardinality() == 0 && updates.Cardinality() == 0 {
		return nil
	}
	// Create a helper method to scan the contents of the key files
	var (
		buf = new(bufio.Reader)
		key struct {
			Address string `json:"address"`
		}
	)

	readAccount := func(path string) *accounts.Account {
		fd, err := os.Open(path)
		if err != nil {
			ac.logger.Trace("Failed to open keystore file", "path", path, "err", err)

			return nil
		}

		defer fd.Close()

		buf.Reset(fd)
		// Parse the address.
		key.Address = ""
		err = json.NewDecoder(buf).Decode(&key)
		addr := types.StringToAddress(key.Address)

		switch {
		case err != nil:
			ac.logger.Debug("Failed to decode keystore key", "path", path, "err", err)
		case addr == types.Address{}:
			ac.logger.Debug("Failed to decode keystore key", "path", path, "err", "missing or zero address")
		default:
			return &accounts.Account{
				Address: addr,
				URL:     accounts.URL{Scheme: KeyStoreScheme, Path: path},
			}
		}

		return nil
	}
	// Process all the file diffs
	start := time.Now().UTC()

	for _, path := range creates.ToSlice() {
		if a := readAccount(path); a != nil {
			ac.add(*a)
		}
	}

	for _, path := range deletes.ToSlice() {
		ac.deleteByFile(path)
	}

	for _, path := range updates.ToSlice() {
		ac.deleteByFile(path)

		if a := readAccount(path); a != nil {
			ac.add(*a)
		}
	}

	end := time.Now().UTC()

	select {
	case ac.notify <- struct{}{}:
	default:
	}
	ac.logger.Trace("Handled keystore changes", "time", end.Sub(start))

	return nil
}
