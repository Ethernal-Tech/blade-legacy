package accounts

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownAccount = errors.New("unknown account")

	ErrWalletClosed = errors.New("wallet closed")

	ErrNoMatch = errors.New("no key for given address or file")
	ErrDecrypt = errors.New("could not decrypt key with given password")
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
