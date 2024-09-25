package e2e

import (
	"testing"

	polycfg "github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/stretchr/testify/require"
)

func TestE2E_BurnContract_Deployed(t *testing.T) {
	contractKey, _ := crypto.GenerateECDSAKey()
	destinationKey, _ := crypto.GenerateECDSAKey()

	contractAddr := contractKey.Address()
	destinationAddr := destinationKey.Address()

	cluster := framework.NewTestCluster(t, 5,
		framework.WithBridges(1),
		framework.WithNativeTokenConfig(nativeTokenNonMintableConfig),
		framework.WithTestRewardToken(),
		framework.WithBurnContract(&polycfg.BurnContractInfo{
			Address:            contractAddr,
			DestinationAddress: destinationAddr,
		}),
	)
	defer cluster.Stop()

	cluster.WaitForReady(t)
	client := cluster.Servers[0].JSONRPC()

	// Get the code for the default deployed burn contract
	code, err := client.GetCode(contractAddr, jsonrpc.LatestBlockNumberOrHash)
	require.NoError(t, err)
	require.NotEqual(t, code, "0x")
}
