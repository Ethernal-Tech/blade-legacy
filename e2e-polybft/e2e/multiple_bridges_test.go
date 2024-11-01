package e2e

import (
	"math/big"
	"path"
	"testing"

	bridgeHelper "github.com/0xPolygon/polygon-edge/command/bridge/helper"
	polycfg "github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

const (
	chainConfigFile = "genesis.json"
)

func TestE2E_Multiple_Bridges_ExternalToInternalERC20TokenTransfer(t *testing.T) {
	const (
		numberOfAccounts      = 5
		numBlockConfirmations = 2
		epochSize             = 40
		sprintSize            = uint64(5)
		numberOfBridges       = 2
	)

	accounts := make([]*crypto.ECDSAKey, numberOfAccounts)

	for i := 0; i < numberOfAccounts; i++ {
		ecdsaKey, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		accounts[i] = ecdsaKey
	}

	cluster := framework.NewTestCluster(t, 5,
		framework.WithTestRewardToken(),
		framework.WithNumBlockConfirmations(numBlockConfirmations),
		framework.WithEpochSize(epochSize),
		framework.WithBridges(numberOfBridges),
		framework.WithSecretsCallback(func(_ []types.Address, tcc *framework.TestClusterConfig) {
			addresses := make([]string, len(accounts))
			for i := 0; i < len(accounts); i++ {
				addresses[i] = accounts[i].Address().String()
			}

			tcc.Premine = append(tcc.Premine, addresses...)
		}))

	defer cluster.Stop()

	cluster.WaitForReady(t)

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFile))
	require.NoError(t, err)

	internalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(cluster.Servers[0].JSONRPC()))
	require.NoError(t, err)

	// delete this
	_ = internalChainTxRelayer

	externalChainTxRelayers := make([]txrelayer.TxRelayer, numberOfBridges)

	for i := 0; i < numberOfBridges; i++ {
		txRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[i].JSONRPCAddr()))
		require.NoError(t, err)
		externalChainTxRelayers[i] = txRelayer
	}

	bridgeConfigs := make([]*polycfg.Bridge, numberOfBridges)
	chainIDs := make([]*big.Int, numberOfBridges)

	for i := 0; i < numberOfBridges; i++ {
		chainID, err := externalChainTxRelayers[i].Client().ChainID()
		require.NoError(t, err)

		chainIDs[i] = chainID
		bridgeConfigs[i] = polybftCfg.Bridge[chainID.Uint64()]
	}

	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	tx := types.NewTx(types.NewLegacyTx(
		types.WithTo(nil),
		types.WithInput(contractsapi.RootERC20.Bytecode),
	))

	rootERC20Tokens := make([]types.Address, numberOfBridges)

	for i, relayer := range externalChainTxRelayers {
		receipt, err := relayer.SendTransaction(tx, deployerKey)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

		rootERC20Tokens[i] = types.Address(receipt.ContractAddress)
		t.Logf("Successfully deployed root ERC20 smart contract on external chain %d at address %s:", chainIDs[i], rootERC20Tokens[i].String())
	}
}
