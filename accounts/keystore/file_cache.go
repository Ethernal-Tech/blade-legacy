package keystore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
)

// fileCache is a cache of files seen during scan of keystore.
type fileCache struct {
	all     mapset.Set[string] // Set of all files from the keystore folder
	lastMod time.Time          // Last time instance when a file was modified
	mu      sync.Mutex
}

func (fc *fileCache) scan(keyDir string) (mapset.Set[string], mapset.Set[string], mapset.Set[string], error) {
	// List all the files from the keystore folder
	files, err := os.ReadDir(keyDir)
	if err != nil {
		return nil, nil, nil, err
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	all := mapset.NewThreadUnsafeSet[string]()
	mods := mapset.NewThreadUnsafeSet[string]()

	var newLastMod time.Time

	for _, fi := range files {
		if nonKeyFile(fi) {
			continue
		}

		path := filepath.Join(keyDir, fi.Name())

		all.Add(path)

		info, err := fi.Info()
		if err != nil {
			return nil, nil, nil, err
		}

		modified := info.ModTime()
		if modified.After(fc.lastMod) {
			mods.Add(path)
		}

		if modified.After(newLastMod) {
			newLastMod = modified
		}
	}

	deletes := fc.all.Difference(all)
	creates := all.Difference(fc.all)
	updates := mods.Difference(creates)

	fc.all, fc.lastMod = all, newLastMod

	return creates, deletes, updates, nil
}

// nonKeyFile ignores editor backups, hidden files and folders/symlinks.
func nonKeyFile(fi os.DirEntry) bool {
	// Skip editor backups and UNIX-style hidden files.
	if strings.HasSuffix(fi.Name(), "~") || strings.HasPrefix(fi.Name(), ".") {
		return true
	}
	// Skip misc special files, directories (yes, symlinks too).
	if fi.IsDir() || !fi.Type().IsRegular() {
		return true
	}

	return false
}
