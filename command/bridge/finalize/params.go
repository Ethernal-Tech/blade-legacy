package finalize

import (
	"fmt"
	"os"

	bridgeHelper "github.com/0xPolygon/polygon-edge/command/bridge/helper"
	"github.com/0xPolygon/polygon-edge/command/helper"
	validatorHelper "github.com/0xPolygon/polygon-edge/command/validator/helper"
	"github.com/0xPolygon/polygon-edge/types"
)

type finalizeParams struct {
	accountDir    string
	accountConfig string
	privateKey    string
	jsonRPC       string
	bladeManager  string
	genesisPath   string
}

func (fp *finalizeParams) validateFlags() error {
	if fp.privateKey == "" {
		return validatorHelper.ValidateSecretFlags(fp.accountDir, fp.accountConfig)
	}

	if fp.bladeManager == "" {
		return bridgeHelper.ErrMandatoryBladeManagerAddr
	}

	if err := types.IsValidAddress(fp.bladeManager); err != nil {
		return fmt.Errorf("invalid blade manager address is provided: %w", err)
	}

	if _, err := os.Stat(fp.genesisPath); err != nil {
		return fmt.Errorf("provided genesis path '%s' is invalid. Error: %w ", fp.genesisPath, err)
	}

	// validate jsonrpc address
	_, err := helper.ParseJSONRPCAddress(fp.jsonRPC)

	return err
}
