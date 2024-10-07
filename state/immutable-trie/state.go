package itrie

import (
	"fmt"

	lru "github.com/hashicorp/golang-lru"

	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
)

type State struct {
	storage Storage
	cache   *lru.Cache
}

func NewState(storage Storage) *State {
	cache, _ := lru.New(128)

	s := &State{
		storage: storage,
		cache:   cache,
	}

	return s
}

func (s *State) NewSnapshot(root types.Hash) (state.Snapshot, error) {
	var (
		t   *Trie
		err error
	)

	if root != types.ZeroHash {
		t, err = s.newTrieAt(root)
		if err != nil {
			return nil, err
		}
	} else {
		t = s.newTrie()
	}

	return &Snapshot{state: s, trie: t}, nil
}

func (s *State) newTrie() *Trie {
	return NewTrie()
}

func (s *State) SetCode(hash types.Hash, code []byte) error {
	return s.storage.SetCode(hash, code)
}

func (s *State) GetCode(hash types.Hash) ([]byte, bool) {
	if hash == types.EmptyCodeHash {
		return []byte{}, true
	}

	return s.storage.GetCode(hash)
}

func (s *State) Has(hash types.Hash) bool {
	if hash == types.EmptyCodeHash {
		return false
	}

	ok, err := s.storage.Has(hash.Bytes())
	if err != nil {
		return false
	}

	return ok
}

func (s *State) Get(hash types.Hash) ([]byte, bool, error) {
	if hash == types.EmptyCodeHash {
		return nil, false, nil
	}

	return s.storage.Get(hash.Bytes())
}

// newTrieAt returns trie with root and if necessary locks state on a trie level
func (s *State) newTrieAt(root types.Hash) (*Trie, error) {
	if root == types.EmptyRootHash {
		// empty state
		return s.newTrie(), nil
	}

	tt, ok := s.cache.Get(root)
	if ok {
		t, ok := tt.(*Trie)
		if !ok {
			return nil, fmt.Errorf("invalid type assertion on root: %s", root)
		}

		return t, nil
	}

	n, ok, err := GetNode(root.Bytes(), s.storage)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage root %s: %w", root, err)
	}

	if !ok {
		return nil, fmt.Errorf("state not found at hash %s", root)
	}

	t := &Trie{
		root: n,
	}

	return t, nil
}

func (s *State) AddState(root types.Hash, t *Trie) {
	s.cache.Add(root, t)
}
