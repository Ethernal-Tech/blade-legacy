package accounts

import (
	"github.com/0xPolygon/polygon-edge/command/accounts/create"
	"github.com/0xPolygon/polygon-edge/command/accounts/insert"
	"github.com/0xPolygon/polygon-edge/command/accounts/update"
	"github.com/spf13/cobra"
)

func GetCommand() *cobra.Command {
	accountCmd := &cobra.Command{
		Use:   "account",
		Short: "Account management command.",
	}

	registerSubcommands(accountCmd)

	return accountCmd
}

func registerSubcommands(baseCmd *cobra.Command) {
	baseCmd.AddCommand(
		// insert new account
		insert.GetCommand(),
		// create new account
		create.GetCommand(),
		// update existing account
		update.GetCommand(),
	)
}
