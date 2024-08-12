package finalize

import (
	"fmt"
	"os"
	"time"

	"github.com/0xPolygon/polygon-edge/command/helper"
	validatorHelper "github.com/0xPolygon/polygon-edge/command/validator/helper"
)

type finalizeParams struct {
	accountDir    string
	accountConfig string
	privateKey    string
	jsonRPC       string
	genesisPath   string
	txTimeout     time.Duration
}

func (fp *finalizeParams) validateFlags() error {
	var err error

	if fp.privateKey == "" {
		return validatorHelper.ValidateSecretFlags(fp.accountDir, fp.accountConfig)
	}

	if _, err := os.Stat(fp.genesisPath); err != nil {
		return fmt.Errorf("provided genesis path '%s' is invalid. Error: %w ", fp.genesisPath, err)
	}

	// validate jsonrpc address
	_, err = helper.ParseJSONRPCAddress(fp.jsonRPC)

	return err
}
