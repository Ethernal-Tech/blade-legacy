package keystore

import (
	"time"

	"github.com/fsnotify/fsnotify"
)

type watcher struct {
	ac       *accountCache
	running  bool // set to true when runloop begins
	runEnded bool // set to true when runloop ends
	starting bool // set to true prior to runloop starting
	quit     chan struct{}
}

func newWatcher(ac *accountCache) *watcher {
	return &watcher{
		ac:   ac,
		quit: make(chan struct{}),
	}
}

func (*watcher) enabled() bool { return true }

func (w *watcher) start() {
	if w.starting || w.running {
		return
	}
	w.starting = true
	go w.loop()
}

func (w *watcher) close() {
	close(w.quit)
}

func (w *watcher) loop() {
	defer func() {
		w.ac.mu.Lock()
		w.running = false
		w.starting = false
		w.runEnded = true
		w.ac.mu.Unlock()
	}()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		//TO DO add log
		return
	}

	defer watcher.Close()

	if err := watcher.Add(w.ac.keydir); err != nil {
		//TO DO logger
		return
	}

	w.ac.mu.Lock()
	w.running = true
	w.ac.mu.Unlock()

	var (
		debounceDuration = 500 * time.Millisecond
		rescanTriggered  = false
		debounce         = time.NewTimer(0)
	)

	if !debounce.Stop() {
		<-debounce.C
	}
	defer debounce.Stop()
	for {
		select {
		case <-w.quit:
			return
		case _, ok := <-watcher.Events:
			if !ok {
				return
			}

			if !rescanTriggered {
				debounce.Reset(debounceDuration)
				rescanTriggered = true
			}

		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// TO DO log.Info("Filesystem watcher error", "err", err)
		case <-debounce.C:
			w.ac.scanAccounts()
			rescanTriggered = false
		}
	}

}
