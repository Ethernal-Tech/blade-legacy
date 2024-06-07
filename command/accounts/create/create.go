package accounts

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/spf13/cobra"
)

var (
	params createParams
)

func GetCommand() *cobra.Command {
	createCmd := &cobra.Command{
		Use:    "create",
		Short:  "Create new account",
		PreRun: runPreRun,
		Run:    runCommand,
	}

	helper.RegisterJSONRPCFlag(createCmd)

	return createCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.Passphrase,
		PassphraseFlag,
		"",
		"passphrase for access to private key",
	)

	cmd.Flags().StringVar(
		&params.ConfigDir,
		ConfigDirFlag,
		"",
		"dir of config",
	)

	_ = cmd.MarkFlagRequired(PassphraseFlag)
}

func runPreRun(cmd *cobra.Command, _ []string) {

}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	scryptN := keystore.StandardScryptN
	scryptP := keystore.StandardScryptP

	if false {
		scryptN = keystore.LightScryptN
		scryptP = keystore.LightScryptP
	}

	account, err := keystore.StoreKey(params.Passphrase, scryptN, scryptP)
	if err != nil {
		outputter.SetError(fmt.Errorf("can't create account"))
	}

	outputter.SetCommandResult(command.Results{&createResult{Address: account.Address, PrivateKeyPath: account.URL.Path}})
}
