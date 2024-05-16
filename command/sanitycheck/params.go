package sanitycheck

import (
	"errors"
	"time"

	"github.com/0xPolygon/polygon-edge/command/loadtest"
)

const (
	epochSizeFlag     = "epoch-size"
	validatorKeysFlag = "validator-keys"
)

var (
	errInvalidEpochSize = errors.New("epoch size must be greater than 0")
)

// sanityCheckParams holds the parameters for the sanity check command
type sanityCheckParams struct {
	mnemonic       string
	jsonRPCAddress string

	receiptsTimeout time.Duration

	epochSize uint64
	toJSON    bool

	validatorKeys []string
}

// validateFlags checks if the provided flags are valid
func (scp *sanityCheckParams) validateFlags() error {
	if scp.mnemonic == "" {
		return loadtest.ErrNoMnemonicProvided
	}

	if scp.epochSize == 0 {
		return errInvalidEpochSize
	}

	return nil
}
