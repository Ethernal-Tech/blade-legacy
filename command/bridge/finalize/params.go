package finalize

import (
	"fmt"
	"os"

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

	if err := types.IsValidAddress(fp.bladeManager, false); err != nil {
		return fmt.Errorf("invalid blade manager address is provided: %w", err)
	}

	if _, err := os.Stat(fp.genesisPath); err != nil {
		return fmt.Errorf("provided genesis path '%s' is invalid. Error: %w ", fp.genesisPath, err)
	}

	// validate jsonrpc address
	_, err := helper.ParseJSONRPCAddress(fp.jsonRPC)

	return err
}
