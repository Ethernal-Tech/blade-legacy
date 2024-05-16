package sanitycheck

import (
	"time"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/command/loadtest"
	"github.com/0xPolygon/polygon-edge/loadtest/sanitycheck"
	"github.com/spf13/cobra"
)

var (
	params sanityCheckParams
)

func GetCommand() *cobra.Command {
	loadTestCmd := &cobra.Command{
		Use:     "sanity-check",
		Short:   "Runs sanity check tests on a specified network",
		PreRunE: preRunCommand,
		Run:     runCommand,
	}

	helper.RegisterJSONRPCFlag(loadTestCmd)

	setFlags(loadTestCmd)

	return loadTestCmd
}

func preRunCommand(cmd *cobra.Command, _ []string) error {
	params.jsonRPCAddress = helper.GetJSONRPCAddress(cmd)

	return params.validateFlags()
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.mnemonic,
		loadtest.MnemonicFlag,
		"",
		"the mnemonic used to fund accounts if needed for the sanity check",
	)

	cmd.Flags().Uint64Var(
		&params.epochSize,
		epochSizeFlag,
		10,
		"epoch size on the network for which the sanity check is run",
	)

	cmd.Flags().DurationVar(
		&params.receiptsTimeout,
		loadtest.ReceiptsTimeoutFlag,
		30*time.Second,
		"the timeout for waiting for transaction receipts",
	)

	cmd.Flags().BoolVar(
		&params.toJSON,
		loadtest.SaveToJSONFlag,
		false,
		"saves results to JSON file",
	)

	cmd.Flags().StringSliceVar(
		&params.validatorKeys,
		validatorKeysFlag,
		nil,
		"private keys of validators on the network for which the sanity check is run",
	)

	_ = cmd.MarkFlagRequired(loadtest.MnemonicFlag)
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)
	defer outputter.WriteOutput()

	sanityCheckRunner, err := sanitycheck.NewSanityCheckTestRunner(
		&sanitycheck.SanityCheckTestConfig{
			Mnemonic:        params.mnemonic,
			JSONRPCUrl:      params.jsonRPCAddress,
			ReceiptsTimeout: params.receiptsTimeout,
			EpochSize:       params.epochSize,
			ValidatorKeys:   params.validatorKeys,
			ResultsToJSON:   params.toJSON,
		},
	)

	if err != nil {
		outputter.SetError(err)

		return
	}

	sanityCheckRunner.Run()
}
