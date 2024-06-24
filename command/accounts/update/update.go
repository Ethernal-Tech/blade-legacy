package update

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/command"
	bridgeHelper "github.com/0xPolygon/polygon-edge/command/bridge/helper"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/spf13/cobra"
)

var (
	params updateParams
)

func GetCommand() *cobra.Command {
	updateCmd := &cobra.Command{
		Use:     "update",
		Short:   "Update passphrase of existing account",
		PreRunE: runPreRun,
		Run:     runCommand,
	}

	setFlags(updateCmd)

	return updateCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.rawAddress,
		addressFlag,
		"",
		"address of account",
	)

	cmd.Flags().StringVar(
		&params.passphrase,
		passphraseFlag,
		"",
		"new passphrase for access to private key",
	)

	cmd.Flags().StringVar(
		&params.oldPassphrase,
		oldPassphraseFlag,
		"",
		"old passphrase to unlock account",
	)

	helper.RegisterJSONRPCFlag(cmd)
}

func runPreRun(cmd *cobra.Command, _ []string) error {
	params.jsonRPC = helper.GetJSONRPCAddress(cmd)

	return params.validateFlags()
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	client, err := jsonrpc.NewEthClient(params.jsonRPC)
	if err != nil {
		outputter.SetError(fmt.Errorf("can't create jsonRPC client: %w", err))

		return
	}

	var isUpdated bool

	if err := client.EndpointCall("personal_updatePassphrase", &isUpdated, params.address,
		params.oldPassphrase, params.passphrase); err != nil {
		outputter.SetError(fmt.Errorf("can't update passphrase: %w", err))

		return
	}

	if isUpdated {
		outputter.WriteCommandResult(&bridgeHelper.MessageResult{Message: "Passphrase updated successfully"})
	} else {
		outputter.SetError(fmt.Errorf("can't update passphrase"))
	}
}
