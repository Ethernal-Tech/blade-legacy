package update

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/hashicorp/go-hclog"
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

	helper.RegisterJSONRPCFlag(updateCmd)
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
}

func runPreRun(cmd *cobra.Command, _ []string) error {
	return params.validateFlags()
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	ks := keystore.NewKeyStore(keystore.DefaultStorage, keystore.LightScryptN, keystore.LightScryptP, hclog.NewNullLogger(), chain.AllForksEnabled.At(0))

	if !ks.HasAddress(params.address) {
		outputter.SetError(fmt.Errorf("this address doesn't exist"))
	} else {
		if err := ks.Update(accounts.Account{Address: params.address}, params.passphrase, params.oldPassphrase); err != nil {
			outputter.SetError(fmt.Errorf("can't update account: %w", err))
		}
	}
}
