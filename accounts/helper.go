package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

func LoadJSON(file string, val interface{}) error {
	content, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(content, val); err != nil {
		if syntaxerr, ok := err.(*json.SyntaxError); ok { //nolint:errorlint
			line := findLine(content, syntaxerr.Offset)

			return fmt.Errorf("JSON syntax error at %v:%v: %w", file, line, err)
		}

		return fmt.Errorf("JSON unmarshal error in %v: %w", file, err)
	}

	return nil
}

func findLine(data []byte, offset int64) (line int) {
	line = 1

	for i, r := range string(data) {
		if int64(i) >= offset {
			return
		}

		if r == '\n' {
			line++
		}
	}

	return
}
