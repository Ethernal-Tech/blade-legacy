package keystore

import (
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hashicorp/go-hclog"
)

type watcher struct {
	logger   hclog.Logger
	ac       *accountCache
	running  bool // set to true when runloop begins
	runEnded bool // set to true when runloop ends
	starting bool // set to true prior to runloop starting
	quit     chan struct{}
}

func newWatcher(ac *accountCache, logger hclog.Logger) *watcher {
	return &watcher{
		logger: logger,
		ac:     ac,
		quit:   make(chan struct{}),
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
		w.logger.Error("Failed to start filesystem watcher", "err", err)
		return
	}

	defer watcher.Close()

	if err := watcher.Add(w.ac.keydir); err != nil {
		if !os.IsNotExist(err) {
			w.logger.Info("Failed to watch keystore folder", "err", err)
		}
		return
	}

	w.logger.Trace("Started watching keystore folder", "folder", w.ac.keydir)
	defer w.logger.Trace("Stopped watching keystore folder")

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
			w.logger.Info("Filesystem watcher error", "err", err)
		case <-debounce.C:
			if err := w.ac.scanAccounts(); err != nil {
				w.logger.Info("loop", "scanAccounts", err)
			}
			rescanTriggered = false
		}
	}
}
