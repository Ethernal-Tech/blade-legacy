package accounts

import (
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/spf13/cobra"
)

var (
	params updateParams
)

func GetCommand() *cobra.Command {
	updateCmd := &cobra.Command{
		Use:     "update",
		Short:   "Update existing account",
		PreRunE: runPreRun,
		RunE:    runCommand,
	}

	helper.RegisterJSONRPCFlag(updateCmd)
	setFlags(updateCmd)

	return updateCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.Address,
		AddressFlag,
		"",
		"address of account",
	)

	cmd.Flags().StringVar(
		&params.Passphrase,
		PassphraseFlag,
		"",
		"passphrase for access to private key",
	)
}

func runPreRun(cmd *cobra.Command, _ []string) error {
	return nil
}

func runCommand(cmd *cobra.Command, _ []string) error {
	return nil
}
