package create

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/hashicorp/go-hclog"
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
		&params.passphrase,
		PassphraseFlag,
		"",
		"passphrase for access to private key",
	)

	cmd.Flags().StringVar(
		&params.configDir,
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

	scryptN := keystore.LightScryptN
	scryptP := keystore.LightScryptP

	ks := keystore.NewKeyStore(keystore.DefaultStorage, scryptN, scryptP, hclog.NewNullLogger())

	account, err := ks.NewAccount(params.passphrase)
	if err != nil {
		outputter.SetError(fmt.Errorf("can't create account"))
	}

	outputter.SetCommandResult(command.Results{&createResult{Address: account.Address, PrivateKeyPath: account.URL.Path}})
}
