package polybft

import (
	"github.com/0xPolygon/polygon-edge/command/rootchain/withdraw"
	"github.com/0xPolygon/polygon-edge/command/sidechain/rewards"
	"github.com/0xPolygon/polygon-edge/command/sidechain/validators"
	sidechainWithdraw "github.com/0xPolygon/polygon-edge/command/sidechain/withdraw"
	"github.com/0xPolygon/polygon-edge/command/validator/registration"
	staking "github.com/0xPolygon/polygon-edge/command/validator/stake"
	unstaking "github.com/0xPolygon/polygon-edge/command/validator/unstake"
	"github.com/0xPolygon/polygon-edge/command/validator/whitelist"
	"github.com/spf13/cobra"
)

func GetCommand() *cobra.Command {
	polybftCmd := &cobra.Command{
		Use:   "polybft",
		Short: "Polybft command",
	}

	polybftCmd.AddCommand(
		// sidechain (validator set) command to unstake on child chain
		unstaking.GetCommand(),
		// sidechain (validator set) command to withdraw stake on child chain
		sidechainWithdraw.GetCommand(),
		// sidechain (reward pool) command to withdraw pending rewards
		rewards.GetCommand(),
		// rootchain (stake manager) command to withdraw stake
		withdraw.GetCommand(),
		// rootchain (supernet manager) command that queries validator info
		validators.GetCommand(),
		// rootchain (supernet manager) whitelist validator
		whitelist.GetCommand(),
		// rootchain (supernet manager) register validator
		registration.GetCommand(),
		// rootchain (stake manager) stake command
		staking.GetCommand(),
	)

	return polybftCmd
}
