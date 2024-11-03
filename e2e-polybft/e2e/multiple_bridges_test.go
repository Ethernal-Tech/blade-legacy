package e2e

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/command/bridge/common"
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
		numberOfBridges       = 1
	)

	accounts := make([]*crypto.ECDSAKey, numberOfAccounts)

	t.Logf("%d accounts were created with the following addresses:", numberOfAccounts)
	for i := 0; i < numberOfAccounts; i++ {
		ecdsaKey, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		accounts[i] = ecdsaKey

		t.Logf("#%d - %s", i+1, accounts[i].Address().String())
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

	externalChainTxRelayers := make([]txrelayer.TxRelayer, numberOfBridges)

	for i := 0; i < numberOfBridges; i++ {
		txRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[i].JSONRPCAddr()))
		require.NoError(t, err)
		externalChainTxRelayers[i] = txRelayer
	}

	bridgeConfigs := make([]*polycfg.Bridge, numberOfBridges)

	internalChainID, err := internalChainTxRelayer.Client().ChainID()
	require.NoError(t, err)

	externalChainIDs := make([]*big.Int, numberOfBridges)

	for i := 0; i < numberOfBridges; i++ {
		chainID, err := externalChainTxRelayers[i].Client().ChainID()
		require.NoError(t, err)

		externalChainIDs[i] = chainID
		bridgeConfigs[i] = polybftCfg.Bridge[chainID.Uint64()]
	}

	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	t.Run("bridge ERC20 tokens", func(t *testing.T) {
		tx := types.NewTx(types.NewLegacyTx(
			types.WithTo(nil),
			types.WithInput(contractsapi.RootERC20.Bytecode),
		))

		wg := sync.WaitGroup{}

		for i := range numberOfBridges {
			wg.Add(1)

			go func(bridgeNum int) {
				defer wg.Done()

				logFunc := func(format string, args ...any) {
					pf := fmt.Sprintf("[%s⇄%s] ", internalChainID.String(), externalChainIDs[bridgeNum].String())
					t.Logf(pf+format, args...)
				}

				receipt, err := externalChainTxRelayers[bridgeNum].SendTransaction(tx, deployerKey)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

				rootERC20Tokens := types.Address(receipt.ContractAddress)
				logFunc("Root ERC20 smart contract was successfully deployed on the external chain %d at address %s", externalChainIDs[i], rootERC20Tokens.String())

				for i := 0; i < numberOfAccounts; i++ {
					err := cluster.Bridges[bridgeNum].Deposit(
						common.ERC20,
						rootERC20Tokens,
						bridgeConfigs[bridgeNum].ExternalERC20PredicateAddr,
						bridgeHelper.TestAccountPrivKey,
						accounts[i].Address().String(),
						"100000000000000000",
						"",
						cluster.Bridges[bridgeNum].JSONRPCAddr(),
						bridgeHelper.TestAccountPrivKey,
						false,
					)

					require.NoError(t, err)

					logFunc("The deposit was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts + 1 {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(i+1)) {
							logFunc("Event %d still not processed", i+1)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				childERC20Token := getChildToken(t, contractsapi.RootERC20Predicate.Abi,
					bridgeConfigs[bridgeNum].ExternalERC20PredicateAddr, rootERC20Tokens, externalChainTxRelayers[bridgeNum])

				logFunc("Child ERC20 smart contract was successfully deployed on the internal chain at address %s", childERC20Token.String())

				for _, account := range accounts {
					balance := erc20BalanceOf(t, account.Address(), childERC20Token, internalChainTxRelayer)
					validBalance, _ := new(big.Int).SetString("100000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the child ERC20 smart contract", account.Address().String(), balance.String())
				}

				for i := 0; i < numberOfAccounts; i++ {
					rawKey, err := accounts[i].MarshallPrivateKey()
					require.NoError(t, err)

					err = cluster.Bridges[bridgeNum].Withdraw(
						common.ERC20,
						hex.EncodeToString(rawKey),
						accounts[i].Address().String(),
						"100000000000000000",
						"",
						cluster.Servers[0].JSONRPCAddr(),
						bridgeConfigs[bridgeNum].InternalERC20PredicateAddr,
						childERC20Token,
						false)
					require.NoError(t, err)

					logFunc("The withdraw was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(i+1)) {
							logFunc("Event %d still not processed", i+1)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				for _, account := range accounts {
					balance := erc20BalanceOf(t, account.Address(), rootERC20Tokens, externalChainTxRelayers[bridgeNum])
					validBalance, _ := new(big.Int).SetString("100000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the root ERC20 smart contract", account.Address().String(), balance.String())
				}
			}(i)
		}

		wg.Wait()
	})
}
