package rootchain

import (
	"github.com/spf13/cobra"

	"github.com/0xPolygon/polygon-edge/command/rootchain/premine"
)

// GetCommand creates "rootchain" helper command
func GetCommand() *cobra.Command {
	rootchainCmd := &cobra.Command{
		Use:   "rootchain",
		Short: "Top level rootchain helper command.",
	}

	rootchainCmd.AddCommand(
		// rootchain premine
		premine.GetCommand(),
	)

	return rootchainCmd
}
