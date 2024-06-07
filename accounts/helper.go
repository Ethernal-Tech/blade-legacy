package accounts

import (
	"errors"
	"fmt"

	"github.com/0xPolygon/polygon-edge/types"
)

var (
	ErrUnknownAccount = errors.New("unknown account")

	ErrUnknownWallet = errors.New("unknown wallet")

	ErrNotSupported = errors.New("not supported")

	ErrInvalidPassphrase = errors.New("invalid password")

	ErrWalletAlreadyOpen = errors.New("wallet already open")

	ErrWalletClosed = errors.New("wallet closed")

	ErrNoMatch = errors.New("no key for given address or file")
	ErrDecrypt = errors.New("could not decrypt key with given password")

	// ErrAccountAlreadyExists is returned if an account attempted to import is
	// already present in the keystore.
	ErrAccountAlreadyExists = errors.New("account already exists")
)

type AuthNeededError struct {
	Needed string
}

func NewAuthNeededError(needed string) error {
	return &AuthNeededError{
		Needed: needed,
	}
}

func (err *AuthNeededError) Error() string {
	return fmt.Sprintf("authentication needed: %s", err.Needed)
}

type AmbiguousAddrError struct {
	Addr    types.Address
	Matches []Account
}

func (err *AmbiguousAddrError) Error() string {
	files := ""
	for i, a := range err.Matches {
		files += a.URL.Path
		if i < len(err.Matches)-1 {
			files += ", "
		}
	}
	return fmt.Sprintf("multiple keys match address (%s)", files)
}
