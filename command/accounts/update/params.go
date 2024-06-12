package update

import (
	"errors"

	"github.com/0xPolygon/polygon-edge/types"
)

const (
	addressFlag       = "address"
	passphraseFlag    = "new-passphrase"
	oldPassphraseFlag = "old-passphrase"
)

type updateParams struct {
	rawAddress    string
	passphrase    string
	oldPassphrase string
	address       types.Address
}

func (up *updateParams) validateFlags() error {
	addr, err := types.IsValidAddress(up.rawAddress, false)
	if err != nil {
		return err
	}

	up.address = addr

	if up.passphrase != up.oldPassphrase {
		return errors.New("same old and new password")
	}

	return nil
}
