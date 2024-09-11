package insert

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/spf13/cobra"
)

var (
	params insertParams
)

func GetCommand() *cobra.Command {
	importCmd := &cobra.Command{
		Use:   "insert",
		Short: "Insert existing key to new account with private key and auth passphrase",
		PreRun: func(cmd *cobra.Command, args []string) {
			params.jsonRPC = helper.GetJSONRPCAddress(cmd)
		},
		Run: runCommand,
	}

	setFlags(importCmd)

	return importCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.privateKey,
		privateKeyFlag,
		"",
		"privateKey key of new account",
	)

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

	if err := client.EndpointCall("personal_importRawKey", &address,
		params.privateKey, params.passphrase); err != nil {
		outputter.SetError(fmt.Errorf("can't import new key: %w", err))

		return
	}

	outputter.SetCommandResult(command.Results{&insertResult{Address: address}})
}
