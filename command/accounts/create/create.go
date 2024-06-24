package create

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
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
		PreRun: func(cmd *cobra.Command, _ []string) {
			params.jsonRPC = helper.GetJSONRPCAddress(cmd)
		},
	}

	setFlags(createCmd)

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
	helper.RegisterJSONRPCFlag(cmd)
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	client, err := jsonrpc.NewEthClient(params.jsonRPC)
	if err != nil {
		outputter.SetError(fmt.Errorf("can't create jsonRPC client: %w", err))

		return
	}

	var address types.Address

	if err := client.EndpointCall("personal_newAccount", &address, params.passphrase); err != nil {
		outputter.SetError(fmt.Errorf("can't create new account: %w", err))

		return
	}

	outputter.SetCommandResult(command.Results{&createResult{Address: address}})
}
