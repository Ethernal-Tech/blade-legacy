package update

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/types"
)

const (
	AddressFlag       = "address"
	PassphraseFlag    = "passphrase"
	OldPassphraseFlag = "old-passphrase"
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
		return fmt.Errorf("same old and new password")
	}

	return nil
}
