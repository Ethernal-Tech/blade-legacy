package create

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/chain"
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
		Use:   "create",
		Short: "Create new account",
		Run:   runCommand,
	}

	helper.RegisterJSONRPCFlag(createCmd)

	return createCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.passphrase,
		passphraseFlag,
		"",
		"passphrase for access to private key",
	)

	_ = cmd.MarkFlagRequired(passphraseFlag)
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	ks := keystore.NewKeyStore(keystore.DefaultStorage,
		keystore.LightScryptN, keystore.LightScryptP, hclog.NewNullLogger(), chain.AllForksEnabled.At(0))

	account, err := ks.NewAccount(params.passphrase)
	if err != nil {
		outputter.SetError(fmt.Errorf("can't create account: %w", err))
	}

	outputter.SetCommandResult(command.Results{&createResult{Address: account.Address}})
}
