package loadtest

import (
	"time"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/loadtest/runner"
	"github.com/spf13/cobra"
)

var (
	params loadTestParams
)

func GetCommand() *cobra.Command {
	loadTestCmd := &cobra.Command{
		Use:     "load-test",
		Short:   "Runs a load test on a specified network",
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
		mnemonicFlag,
		"",
		"the mnemonic used to generate and fund virtual users",
	)

	cmd.Flags().StringVar(
		&params.loadTestType,
		loadTestTypeFlag,
		"eoa",
		"the type of load test to run (supported types: eoa, erc20, erc721)",
	)

	cmd.Flags().StringVar(
		&params.loadTestName,
		loadTestNameFlag,
		"load test",
		"the name of the load test",
	)

	cmd.Flags().IntVar(
		&params.vus,
		vusFlag,
		1,
		"the number of virtual users",
	)

	cmd.Flags().IntVar(
		&params.txsPerUser,
		txsPerUserFlag,
		1,
		"the number of transactions per virtual user",
	)

	cmd.Flags().BoolVar(
		&params.dynamicTxs,
		dynamicTxsFlag,
		false,
		"indicates whether the load test should generate dynamic transactions",
	)

	cmd.Flags().DurationVar(
		&params.receiptsTimeout,
		receiptsTimeoutFlag,
		30*time.Second,
		"the timeout for waiting for transaction receipts",
	)

	cmd.Flags().DurationVar(
		&params.txPoolTimeout,
		txPoolTimeoutFlag,
		10*time.Minute,
		"the timeout for waiting for the transaction pool to empty",
	)

	cmd.Flags().BoolVar(
		&params.toJSON,
		saveToJSONFlag,
		false,
		"saves results to JSON file",
	)

	cmd.Flags().BoolVar(
		&params.waitForTxPoolToEmpty,
		waitForTxPoolToEmptyFlag,
		false,
		"waits for tx pool to empty before collecting results",
	)

	cmd.Flags().IntVar(
		&params.batchSize,
		batchSizeFlag,
		1,
		"size of a batch of transactions to send to rpc node",
	)

	_ = cmd.MarkFlagRequired(mnemonicFlag)
	_ = cmd.MarkFlagRequired(loadTestTypeFlag)
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)
	defer outputter.WriteOutput()

	loadTestRunner := &runner.LoadTestRunner{}

	err := loadTestRunner.Run(runner.LoadTestConfig{
		Mnemonnic:            params.mnemonic,
		LoadTestType:         params.loadTestType,
		LoadTestName:         params.loadTestName,
		JSONRPCUrl:           params.jsonRPCAddress,
		ReceiptsTimeout:      params.receiptsTimeout,
		TxPoolTimeout:        params.txPoolTimeout,
		VUs:                  params.vus,
		TxsPerUser:           params.txsPerUser,
		BatchSize:            params.batchSize,
		DynamicTxs:           params.dynamicTxs,
		ResultsToJSON:        params.toJSON,
		WaitForTxPoolToEmpty: params.waitForTxPoolToEmpty,
	})

	if err != nil {
		outputter.SetError(err)
	}
}
