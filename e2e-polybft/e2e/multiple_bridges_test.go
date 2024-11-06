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
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	helperCommon "github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

const (
	chainConfigFile   = "genesis.json"
	nativeTokenConfig = "Blade:BLD:18:false:1337"
)

// The purpose of this test is to verify the correctness of bridging different token types (ERC20, ERC721, ERC1155) between
// an internal chain and potentially multiple external chains. The external chains represent the source chains of the tokens.
// This means that token creation (minting) is performed on them. The test content and flow is relatively straightforward. The
// test is divided into three sub-tests, each reflecting the bridging of one token type. After creating accounts and launching
// a cluster with appropriate configuration parameters, sequential execution of each sub-test begins. An important note is that
// by adjusting the "ESP constants", sub-tests can be run individually as well as in different combinations. The sub-test flow
// for each ERC token is the same. After deploying the ERC (20/721/1155) smart contract on the external chain, a deposit, or
// bridging, of tokens to the internal chain is performed. Once it's established that all expected events have been processed,
// the token state on the internal chain is verified to match expectations. The verification method depends on the token type.
// If the previous condition is met, a withdraw, or bridging, of tokens back to the external chain is performed. As in the case
// of the internal chain, after confirming that all expected events have been processed, the token state is verified. If all
// previous conditions are satisfied, the sub-test is successful. Everything previously described for the sub-test is performed
// concurrently for each bridge, that is, for each relation internal chain - one of the external chains.
func TestE2E_Multiple_Bridges_ExternalToInternalTokenTransfer(t *testing.T) {
	const (
		// This also represents the number of deposit/withdraw transactions that will be made.
		numberOfAccounts = 5
		// Necessary, since external chains do not have instant finality.
		numBlockConfirmations = 2
		// Number of blocks after which the validator set is changed.
		epochSize = 40
		// Number of bridges, and therefore the number of external chains.
		numberOfBridges = 1
	)

	// Since the success of the test is partially based on sequential checks of successfully processed events, the
	// following constants represent the starting points for these checks. In other words, the events starting point
	// (ESP) is the ID of the first event in the sequence. Starting points are defined for each of the ERC standards,
	// as well as for internal and external chains. The values can be adjusted so a specific sub-test can be excluded
	// with ease. If all tests are run in sequence, the values should be as follows:
	//  - erc20ExternalESP   = 1
	//  - erc20InternalESP   = 1
	//  - erc721ExternalESP  = numberOfAccounts + 1
	//  - erc721InternalESP  = numberOfAccounts + 2
	//  - erc1155ExternalESP = numberOfAccounts * 2 + 1
	//  - erc1155InternalESP = numberOfAccounts * 2 + 3
	const (
		erc20ExternalESP   = 1
		erc20InternalESP   = 1
		erc721ExternalESP  = numberOfAccounts + 1
		erc721InternalESP  = numberOfAccounts + 2
		erc1155ExternalESP = numberOfAccounts*2 + 1
		erc1155InternalESP = numberOfAccounts*2 + 3
	)

	accounts := make([]*crypto.ECDSAKey, numberOfAccounts)

	t.Logf("%d accounts were created with the following addresses:", numberOfAccounts)
	for i := 0; i < numberOfAccounts; i++ {
		ecdsaKey, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		accounts[i] = ecdsaKey

		t.Logf("#%d - %s", i+1, accounts[i].Address().String())
	}

	// Creating a cluster with configuration parameters and premining funds for previously generated addresses (to be able
	// to pay fees). Validators (5) are pre-funded by default, allowing them to stake and participate in the consensus.
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

	// Creating a relayer to allow transactions to be sent to the internal chain (Servers[0] represents the first of the
	// five validators set by cluster configuration parameters).
	internalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(cluster.Servers[0].JSONRPC()))
	require.NoError(t, err)

	externalChainTxRelayers := make([]txrelayer.TxRelayer, numberOfBridges)

	// Creating a relayers to allow transactions to be sent to the external chains. Since we have an external chain for
	// each bridge, a relay is created for each of these chains.
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

	// A private key (and its corresponding address) that is used for all external chain activities. The address comes
	// pre-funded on external chain by default.
	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	t.Run("bridge ERC20 tokens", func(t *testing.T) {
		tx := types.NewTx(types.NewLegacyTx(
			types.WithTo(nil),
			types.WithInput(contractsapi.RootERC20.Bytecode),
		))

		wg := sync.WaitGroup{}

		// Creating a goroutine for each bridge, that is, for each relation internal chain - one of the external chains
		// and processing bridging operations for each of them concurrently.
		for i := range numberOfBridges {
			wg.Add(1)

			go func(bridgeNum int) {
				defer wg.Done()

				logFunc := func(format string, args ...any) {
					pf := fmt.Sprintf("[%s⇄%s] ", internalChainID.String(), externalChainIDs[bridgeNum].String())
					t.Logf(pf+format, args...)
				}

				// Deploying the ERC20 smart contract to the external chain.
				receipt, err := externalChainTxRelayers[bridgeNum].SendTransaction(tx, deployerKey)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

				rootERC20Token := types.Address(receipt.ContractAddress)
				logFunc("Root ERC20 smart contract was successfully deployed on the external chain %d at address %s", externalChainIDs[i], rootERC20Token.String())

				// For each account, depositing (bridging) 100000000000000000 WEI (0.1 ETH) from the external to the internal chain.
				for i := 0; i < numberOfAccounts; i++ {
					err := cluster.Bridges[bridgeNum].Deposit(
						common.ERC20,
						rootERC20Token,
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

				// Verifying that all events have been successfully processed on the internal chain. The number of these events is equal
				// to the number of deposits (number of accounts) plus 1, which comes from mapping the root ERC20 smart contract on the
				// external chain to the child ERC20 smart contract on the internal chain.
				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts + 1 {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(erc20InternalESP+i)) {
							logFunc("Event %d still not processed", erc20InternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				childERC20Token := getChildToken(t, contractsapi.RootERC20Predicate.Abi,
					bridgeConfigs[bridgeNum].ExternalERC20PredicateAddr, rootERC20Token, externalChainTxRelayers[bridgeNum])

				logFunc("Child ERC20 smart contract was successfully deployed on the internal chain at address %s", childERC20Token.String())

				// Verifying that for each account the token balance on the child ERC20 smart contract (internal chain) is equal to the
				// transferred (bridged) amount (0.1 ETH)
				for _, account := range accounts {
					balance := erc20BalanceOf(t, account.Address(), childERC20Token, internalChainTxRelayer)
					validBalance, _ := new(big.Int).SetString("100000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the child ERC20 smart contract", account.Address().String(), balance.String())
				}

				// For each account, withdrawing (bridging back) 100000000000000000 WEI (0.1 ETH) from the internal to the external chain.
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

				// Verifying that all events have been successfully processed on the external chain. The number of these events is equal
				// to the number of withdraws (number of accounts). The mapping event does not exist in this case.
				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(erc20ExternalESP+i)) {
							logFunc("Event %d still not processed", erc20ExternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				// Verifying that for each account the token balance on the root ERC20 smart contract (external chain) is equal to the
				// transferred (bridged back) amount (0.1 ETH)
				for _, account := range accounts {
					balance := erc20BalanceOf(t, account.Address(), rootERC20Token, externalChainTxRelayers[bridgeNum])
					validBalance, _ := new(big.Int).SetString("100000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the root ERC20 smart contract", account.Address().String(), balance.String())
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("bridge ERC721 tokens", func(t *testing.T) {
		tx := types.NewTx(types.NewLegacyTx(
			types.WithTo(nil),
			types.WithInput(contractsapi.RootERC721.Bytecode),
		))

		wg := sync.WaitGroup{}

		// Creating a goroutine for each bridge, that is, for each relation internal chain - one of the external chains
		// and processing bridging operations for each of them concurrently.
		for i := range numberOfBridges {
			wg.Add(1)

			go func(bridgeNum int) {
				defer wg.Done()

				logFunc := func(format string, args ...any) {
					pf := fmt.Sprintf("[%s⇄%s] ", internalChainID.String(), externalChainIDs[bridgeNum].String())
					t.Logf(pf+format, args...)
				}

				// Deploying the ERC721 smart contract to the external chain.
				receipt, err := externalChainTxRelayers[bridgeNum].SendTransaction(tx, deployerKey)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

				rootERC721Token := types.Address(receipt.ContractAddress)
				logFunc("Root ERC721 smart contract was successfully deployed on the external chain %d at address %s", externalChainIDs[i], rootERC721Token.String())

				// For each account, depositing (bridging) ERC721 token from the external to the internal chain.
				for i := 0; i < numberOfAccounts; i++ {
					err := cluster.Bridges[bridgeNum].Deposit(
						common.ERC721,
						rootERC721Token,
						bridgeConfigs[bridgeNum].ExternalERC721PredicateAddr,
						bridgeHelper.TestAccountPrivKey,
						accounts[i].Address().String(),
						"",
						fmt.Sprintf("%d", i),
						cluster.Bridges[bridgeNum].JSONRPCAddr(),
						bridgeHelper.TestAccountPrivKey,
						false,
					)

					require.NoError(t, err)

					logFunc("The deposit was made for the account %s", accounts[i].Address().String())
				}

				// Verifying that all events have been successfully processed on the internal chain. The number of these events is equal
				// to the number of deposits (number of accounts) plus 1, which comes from mapping the root ERC721 smart contract on the
				// external chain to the child ERC721 smart contract on the internal chain.
				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts + 1 {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(erc721InternalESP+i)) {
							logFunc("Event %d still not processed", erc721InternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				childERC721Token := getChildToken(t, contractsapi.RootERC721Predicate.Abi,
					bridgeConfigs[bridgeNum].ExternalERC721PredicateAddr, rootERC721Token, externalChainTxRelayers[bridgeNum])

				logFunc("Child ERC721 smart contract was successfully deployed on the internal chain at address %s", childERC721Token.String())

				// Verifying that each account owns corresponding token on the child ERC721 smart contract (internal chain).
				for i, account := range accounts {
					owner := erc721OwnerOf(t, big.NewInt(int64(i)), childERC721Token, internalChainTxRelayer)
					require.Equal(t, account.Address(), owner)

					logFunc("Account %s is the owner of ERC721 token with ID %d on the internal chain", account.Address().String(), i)
				}

				// For each account, withdrawing (bridging back) ERC721 token from the internal to the external chain.
				for i := 0; i < numberOfAccounts; i++ {
					rawKey, err := accounts[i].MarshallPrivateKey()
					require.NoError(t, err)

					err = cluster.Bridges[bridgeNum].Withdraw(
						common.ERC721,
						hex.EncodeToString(rawKey),
						accounts[i].Address().String(),
						"",
						fmt.Sprintf("%d", i),
						cluster.Servers[0].JSONRPCAddr(),
						bridgeConfigs[bridgeNum].InternalERC721PredicateAddr,
						childERC721Token,
						false)
					require.NoError(t, err)

					logFunc("The withdraw was made for the account %s", accounts[i].Address().String())
				}

				// Verifying that all events have been successfully processed on the external chain. The number of these events is equal
				// to the number of withdraws (number of accounts). The mapping event does not exist in this case.
				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(erc721ExternalESP+i)) {
							logFunc("Event %d still not processed", erc721ExternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				// Verifying that each account owns corresponding token on the root ERC721 smart contract (external chain).
				for i, account := range accounts {
					owner := erc721OwnerOf(t, big.NewInt(int64(i)), rootERC721Token, externalChainTxRelayers[bridgeNum])
					require.Equal(t, account.Address(), owner)

					logFunc("Account %s is the owner of ERC721 token with ID %d on the external chain", account.Address().String(), i)
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("bridge ERC1155 tokens", func(t *testing.T) {
		tx := types.NewTx(types.NewLegacyTx(
			types.WithTo(nil),
			types.WithInput(contractsapi.RootERC1155.Bytecode),
		))

		wg := sync.WaitGroup{}

		// Creating a goroutine for each bridge, that is, for each relation internal chain - one of the external chains
		// and processing bridging operations for each of them concurrently.
		for i := range numberOfBridges {
			wg.Add(1)

			go func(bridgeNum int) {
				defer wg.Done()

				logFunc := func(format string, args ...any) {
					pf := fmt.Sprintf("[%s⇄%s] ", internalChainID.String(), externalChainIDs[bridgeNum].String())
					t.Logf(pf+format, args...)
				}

				// Deploying the ERC1155 smart contract to the external chain.
				receipt, err := externalChainTxRelayers[bridgeNum].SendTransaction(tx, deployerKey)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

				rootERC1155Token := types.Address(receipt.ContractAddress)
				logFunc("Root ERC1155 smart contract was successfully deployed on the external chain %d at address %s", externalChainIDs[i], rootERC1155Token.String())

				// For each account, depositing (bridging) ERC1155 (0.5 ETH) token from the external to the internal chain.
				for i := 0; i < numberOfAccounts; i++ {
					err := cluster.Bridges[bridgeNum].Deposit(
						common.ERC1155,
						rootERC1155Token,
						bridgeConfigs[bridgeNum].ExternalERC1155PredicateAddr,
						bridgeHelper.TestAccountPrivKey,
						accounts[i].Address().String(),
						"500000000000000000",
						fmt.Sprintf("%d", i),
						cluster.Bridges[bridgeNum].JSONRPCAddr(),
						bridgeHelper.TestAccountPrivKey,
						false,
					)

					require.NoError(t, err)

					logFunc("The deposit was made for the account %s", accounts[i].Address().String())
				}

				// Verifying that all events have been successfully processed on the internal chain. The number of these events is equal
				// to the number of deposits (number of accounts) plus 1, which comes from mapping the root ERC1155 smart contract on the
				// external chain to the child ERC1155 smart contract on the internal chain.
				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts + 1 {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(erc1155InternalESP+i)) {
							logFunc("Event %d still not processed", erc1155InternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				childERC1155Token := getChildToken(t, contractsapi.RootERC1155Predicate.Abi,
					bridgeConfigs[bridgeNum].ExternalERC1155PredicateAddr, rootERC1155Token, externalChainTxRelayers[bridgeNum])

				logFunc("Child ERC1155 smart contract was successfully deployed on the internal chain at address %s", childERC1155Token.String())

				// Verifying that for each account the token balance on the child ERC1155 smart contract (internal chain) is equal to the
				// transferred (bridged) amount (0.5 ETH).
				for i, account := range accounts {
					balanceOfFn := &contractsapi.BalanceOfChildERC1155Fn{
						Account: account.Address(),
						ID:      big.NewInt(int64(i)),
					}

					balanceInput, err := balanceOfFn.EncodeAbi()
					require.NoError(t, err)

					balanceRaw, err := internalChainTxRelayer.Call(types.ZeroAddress, childERC1155Token, balanceInput)
					require.NoError(t, err)

					balance, err := helperCommon.ParseUint256orHex(&balanceRaw)
					require.NoError(t, err)

					validBalance, _ := new(big.Int).SetString("500000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the child ERC1155 smart contract", account.Address().String(), balance.String())
				}

				// For each account, withdrawing (bridging back) ERC1155 token from the internal to the external chain.
				for i := 0; i < numberOfAccounts; i++ {
					rawKey, err := accounts[i].MarshallPrivateKey()
					require.NoError(t, err)

					err = cluster.Bridges[bridgeNum].Withdraw(
						common.ERC1155,
						hex.EncodeToString(rawKey),
						accounts[i].Address().String(),
						"500000000000000000",
						fmt.Sprintf("%d", i),
						cluster.Servers[0].JSONRPCAddr(),
						bridgeConfigs[bridgeNum].InternalERC1155PredicateAddr,
						childERC1155Token,
						false)
					require.NoError(t, err)

					logFunc("The withdraw was made for the account %s", accounts[i].Address().String())
				}

				// Verifying that all events have been successfully processed on the external chain. The number of these events is equal
				// to the number of withdraws (number of accounts). The mapping event does not exist in this case.
				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(erc1155ExternalESP+i)) {
							logFunc("Event %d still not processed", erc1155ExternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				// Verifying that for each account the token balance on the root ERC1155 smart contract (external chain) is equal to the
				// transferred (bridged) amount (0.5 ETH).
				for i, account := range accounts {
					balanceOfFn := &contractsapi.BalanceOfChildERC1155Fn{
						Account: account.Address(),
						ID:      big.NewInt(int64(i)),
					}

					balanceInput, err := balanceOfFn.EncodeAbi()
					require.NoError(t, err)

					balanceRaw, err := externalChainTxRelayers[bridgeNum].Call(types.ZeroAddress, rootERC1155Token, balanceInput)
					require.NoError(t, err)

					balance, err := helperCommon.ParseUint256orHex(&balanceRaw)
					require.NoError(t, err)

					validBalance, _ := new(big.Int).SetString("500000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the root ERC1155 smart contract", account.Address().String(), balance.String())
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestE2E_Multiple_Bridges_InternalToExternalTokenTransfer(t *testing.T) {
	const (
		numberOfAccounts      = 5
		numBlockConfirmations = 2
		epochSize             = 40
		numberOfBridges       = 1
	)

	// Since the success of the test is partially based on sequential checks of successfully processed events, the
	// following constants represent the starting points for these checks. In other words, the events starting point
	// (ESP) is the ID of the first event in the sequence. Starting points are defined for each of the ERC standards,
	// as well as for internal and external chains. The values can be adjusted so a specific sub-test can be excluded
	// with ease. If all tests are run in sequence, the values should be as follows:
	//  - erc20ExternalESP   = 1
	//  - erc20InternalESP   = 1
	//  - erc721ExternalESP  = numberOfAccounts + 2
	//  - erc721InternalESP  = numberOfAccounts + 1
	//  - erc1155ExternalESP = numberOfAccounts * 2 + 3
	//  - erc1155InternalESP = numberOfAccounts * 2 + 1
	const (
		erc20ExternalESP   = 1
		erc20InternalESP   = 1
		erc721ExternalESP  = numberOfAccounts + 2
		erc721InternalESP  = numberOfAccounts + 1
		erc1155ExternalESP = numberOfAccounts*2 + 3
		erc1155InternalESP = numberOfAccounts*2 + 1
	)

	accounts := make([]*crypto.ECDSAKey, numberOfAccounts)

	t.Logf("%d accounts were created with the following addresses:", numberOfAccounts)
	for i := 0; i < numberOfAccounts; i++ {
		ecdsaKey, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		accounts[i] = ecdsaKey

		t.Logf("#%d - %s", i+1, accounts[i].Address().String())
	}

	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	cluster := framework.NewTestCluster(t, 5,
		framework.WithTestRewardToken(),
		framework.WithNumBlockConfirmations(numBlockConfirmations),
		framework.WithEpochSize(epochSize),
		framework.WithBridges(numberOfBridges),
		framework.WithSecretsCallback(func(_ []types.Address, tcc *framework.TestClusterConfig) {
			addresses := make([]string, len(accounts)+1)
			for i := 0; i < len(accounts); i++ {
				addresses[i] = accounts[i].Address().String()
			}

			addresses[len(accounts)] = deployerKey.Address().String()

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

				receipt, err := internalChainTxRelayer.SendTransaction(tx, deployerKey)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

				rootERC20Token := types.Address(receipt.ContractAddress)
				logFunc("Root ERC20 smart contract was successfully deployed on the internal chain %d at address %s", internalChainID, rootERC20Token.String())

				for i := 0; i < numberOfAccounts; i++ {
					err := cluster.Bridges[bridgeNum].Deposit(
						common.ERC20,
						rootERC20Token,
						bridgeConfigs[bridgeNum].InternalMintableERC20PredicateAddr,
						bridgeHelper.TestAccountPrivKey,
						accounts[i].Address().String(),
						"100000000000000000",
						"",
						cluster.Servers[0].JSONRPCAddr(),
						bridgeHelper.TestAccountPrivKey,
						false,
					)

					require.NoError(t, err)

					logFunc("The deposit was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts + 1 {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(erc20ExternalESP+i)) {
							logFunc("Event %d still not processed", erc20ExternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				childERC20Token := getChildToken(t, contractsapi.RootERC20Predicate.Abi,
					bridgeConfigs[bridgeNum].InternalMintableERC20PredicateAddr, rootERC20Token, internalChainTxRelayer)

				logFunc("Child ERC20 smart contract was successfully deployed on the external chain at address %s", childERC20Token.String())

				for _, account := range accounts {
					balance := erc20BalanceOf(t, account.Address(), childERC20Token, externalChainTxRelayers[bridgeNum])
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
						cluster.Bridges[bridgeNum].JSONRPCAddr(),
						bridgeConfigs[bridgeNum].ExternalMintableERC20PredicateAddr,
						childERC20Token,
						false)
					require.NoError(t, err)

					logFunc("The withdraw was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(erc20InternalESP+i)) {
							logFunc("Event %d still not processed", erc20InternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				for _, account := range accounts {
					balance := erc20BalanceOf(t, account.Address(), rootERC20Token, internalChainTxRelayer)
					validBalance, _ := new(big.Int).SetString("100000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the root ERC20 smart contract", account.Address().String(), balance.String())
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("bridge ERC721 tokens", func(t *testing.T) {
		tx := types.NewTx(types.NewLegacyTx(
			types.WithTo(nil),
			types.WithInput(contractsapi.RootERC721.Bytecode),
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

				receipt, err := internalChainTxRelayer.SendTransaction(tx, deployerKey)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

				rootERC721Token := types.Address(receipt.ContractAddress)
				logFunc("Root ERC721 smart contract was successfully deployed on the internal chain %d at address %s", internalChainID, rootERC721Token.String())

				for i := 0; i < numberOfAccounts; i++ {
					err := cluster.Bridges[bridgeNum].Deposit(
						common.ERC721,
						rootERC721Token,
						bridgeConfigs[bridgeNum].InternalMintableERC721PredicateAddr,
						bridgeHelper.TestAccountPrivKey,
						accounts[i].Address().String(),
						"",
						fmt.Sprintf("%d", i),
						cluster.Servers[0].JSONRPCAddr(),
						bridgeHelper.TestAccountPrivKey,
						false,
					)

					require.NoError(t, err)

					logFunc("The deposit was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts + 1 {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(erc721ExternalESP+i)) {
							logFunc("Event %d still not processed", erc721ExternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				childERC721Token := getChildToken(t, contractsapi.RootERC721Predicate.Abi,
					bridgeConfigs[bridgeNum].InternalMintableERC721PredicateAddr, rootERC721Token, internalChainTxRelayer)

				logFunc("Child ERC721 smart contract was successfully deployed on the external chain at address %s", childERC721Token.String())

				for i, account := range accounts {
					owner := erc721OwnerOf(t, big.NewInt(int64(i)), childERC721Token, externalChainTxRelayers[bridgeNum])
					require.Equal(t, account.Address(), owner)

					logFunc("Account %s is the owner of ERC721 token with ID %d on the external chain", account.Address().String(), i)
				}

				for i := 0; i < numberOfAccounts; i++ {
					rawKey, err := accounts[i].MarshallPrivateKey()
					require.NoError(t, err)

					err = cluster.Bridges[bridgeNum].Withdraw(
						common.ERC721,
						hex.EncodeToString(rawKey),
						accounts[i].Address().String(),
						"",
						fmt.Sprintf("%d", i),
						cluster.Bridges[bridgeNum].JSONRPCAddr(),
						bridgeConfigs[bridgeNum].ExternalMintableERC721PredicateAddr,
						childERC721Token,
						false)
					require.NoError(t, err)

					logFunc("The withdraw was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(erc721InternalESP+i)) {
							logFunc("Event %d still not processed", erc721InternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				for i, account := range accounts {
					owner := erc721OwnerOf(t, big.NewInt(int64(i)), rootERC721Token, internalChainTxRelayer)
					require.Equal(t, account.Address(), owner)

					logFunc("Account %s is the owner of ERC721 token with ID %d on the internal chain", account.Address().String(), i)
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("bridge ERC1155 tokens", func(t *testing.T) {
		tx := types.NewTx(types.NewLegacyTx(
			types.WithTo(nil),
			types.WithInput(contractsapi.RootERC1155.Bytecode),
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

				receipt, err := internalChainTxRelayer.SendTransaction(tx, deployerKey)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

				rootERC1155Token := types.Address(receipt.ContractAddress)
				logFunc("Root ERC1155 smart contract was successfully deployed on the internal chain %d at address %s", internalChainID, rootERC1155Token.String())

				for i := 0; i < numberOfAccounts; i++ {
					err := cluster.Bridges[bridgeNum].Deposit(
						common.ERC1155,
						rootERC1155Token,
						bridgeConfigs[bridgeNum].InternalMintableERC1155PredicateAddr,
						bridgeHelper.TestAccountPrivKey,
						accounts[i].Address().String(),
						"500000000000000000",
						fmt.Sprintf("%d", i),
						cluster.Servers[0].JSONRPCAddr(),
						bridgeHelper.TestAccountPrivKey,
						false,
					)

					require.NoError(t, err)

					logFunc("The deposit was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts + 1 {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(erc1155ExternalESP+i)) {
							logFunc("Event %d still not processed", erc1155ExternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				childERC1155Token := getChildToken(t, contractsapi.RootERC1155Predicate.Abi,
					bridgeConfigs[bridgeNum].InternalMintableERC1155PredicateAddr, rootERC1155Token, internalChainTxRelayer)

				logFunc("Child ERC1155 smart contract was successfully deployed on the external chain at address %s", childERC1155Token.String())

				for i, account := range accounts {
					balanceOfFn := &contractsapi.BalanceOfChildERC1155Fn{
						Account: account.Address(),
						ID:      big.NewInt(int64(i)),
					}

					balanceInput, err := balanceOfFn.EncodeAbi()
					require.NoError(t, err)

					balanceRaw, err := externalChainTxRelayers[bridgeNum].Call(types.ZeroAddress, childERC1155Token, balanceInput)
					require.NoError(t, err)

					balance, err := helperCommon.ParseUint256orHex(&balanceRaw)
					require.NoError(t, err)

					validBalance, _ := new(big.Int).SetString("500000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the child ERC1155 smart contract", account.Address().String(), balance.String())
				}

				for i := 0; i < numberOfAccounts; i++ {
					rawKey, err := accounts[i].MarshallPrivateKey()
					require.NoError(t, err)

					err = cluster.Bridges[bridgeNum].Withdraw(
						common.ERC1155,
						hex.EncodeToString(rawKey),
						accounts[i].Address().String(),
						"500000000000000000",
						fmt.Sprintf("%d", i),
						cluster.Bridges[bridgeNum].JSONRPCAddr(),
						bridgeConfigs[bridgeNum].ExternalMintableERC1155PredicateAddr,
						childERC1155Token,
						false)
					require.NoError(t, err)

					logFunc("The withdraw was made for the account %s", accounts[i].Address().String())
				}

				require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
					for i := range numberOfAccounts {
						if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(erc1155InternalESP+i)) {
							logFunc("Event %d still not processed", erc1155InternalESP+i)
							return false
						}
					}

					logFunc("All events are successfully processed")
					return true
				}))

				for i, account := range accounts {
					balanceOfFn := &contractsapi.BalanceOfChildERC1155Fn{
						Account: account.Address(),
						ID:      big.NewInt(int64(i)),
					}

					balanceInput, err := balanceOfFn.EncodeAbi()
					require.NoError(t, err)

					balanceRaw, err := internalChainTxRelayer.Call(types.ZeroAddress, rootERC1155Token, balanceInput)
					require.NoError(t, err)

					balance, err := helperCommon.ParseUint256orHex(&balanceRaw)
					require.NoError(t, err)

					validBalance, _ := new(big.Int).SetString("500000000000000000", 10)

					require.Equal(t, validBalance, balance)

					logFunc("Account %s has the balance of %s tokens on the root ERC1155 smart contract", account.Address().String(), balance.String())
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestE2E_Multiple_Bridges_ExternalToInternalNativeTokenTransfer(t *testing.T) {
	const (
		numberOfAccounts      = 5
		numBlockConfirmations = 2
		epochSize             = 40
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
		framework.WithNativeTokenConfig(nativeTokenConfig))

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

	wg := sync.WaitGroup{}

	for i := range numberOfBridges {
		wg.Add(1)

		go func(bridgeNum int) {
			defer wg.Done()

			logFunc := func(format string, args ...any) {
				pf := fmt.Sprintf("[%s⇄%s] ", internalChainID.String(), externalChainIDs[bridgeNum].String())
				t.Logf(pf+format, args...)
			}

			rootERC20Token := bridgeConfigs[bridgeNum].ExternalNativeERC20Addr
			logFunc("Root ERC20 smart contract was successfully deployed on the external chain %d at address %s", externalChainIDs[i], rootERC20Token.String())

			balances := make([]*big.Int, numberOfAccounts)

			logFunc("Balances of accounts on internal chain before deposit:")
			for i := 0; i < numberOfAccounts; i++ {
				balance, err := internalChainTxRelayer.Client().GetBalance(accounts[i].Address(), jsonrpc.LatestBlockNumberOrHash)
				require.NoError(t, err)

				logFunc("#%d - %s : %s Wei", i+1, accounts[i].Address().String(), balance.String())
				balances[i] = balance
			}

			for i := 0; i < numberOfAccounts; i++ {
				err = cluster.Bridges[bridgeNum].Deposit(
					common.ERC20,
					rootERC20Token,
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
				for i := range numberOfAccounts {
					if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(i+1)) {
						logFunc("Event %d still not processed", i+1)
						return false
					}
				}

				logFunc("All events are successfully processed")
				return true
			}))

			logFunc("Balances of accounts on internal chain after deposit:")
			for i := 0; i < numberOfAccounts; i++ {
				balance, err := internalChainTxRelayer.Client().GetBalance(accounts[i].Address(), jsonrpc.LatestBlockNumberOrHash)
				require.NoError(t, err)

				logFunc("#%d - %s : %s Wei", i+1, accounts[i].Address().String(), balance.String())
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
					contracts.NativeERC20TokenContract,
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

			logFunc("Balances of accounts on internal chain after withdraw:")
			for i := 0; i < numberOfAccounts; i++ {
				balance, err := internalChainTxRelayer.Client().GetBalance(accounts[i].Address(), jsonrpc.LatestBlockNumberOrHash)
				require.NoError(t, err)

				logFunc("#%d - %s : %s Wei", i+1, accounts[i].Address().String(), balance.String())
				require.Equal(t, balances[i], balance)
			}
		}(i)
	}
	wg.Wait()
}

func TestE2E_Multiple_Bridges_InternalToExternalNativeTokenTransfer(t *testing.T) {
	const (
		numberOfAccounts      = 5
		numBlockConfirmations = 2
		epochSize             = 40
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

	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	cluster := framework.NewTestCluster(t, 5,
		framework.WithTestRewardToken(),
		framework.WithNumBlockConfirmations(numBlockConfirmations),
		framework.WithEpochSize(epochSize),
		framework.WithBridges(numberOfBridges),
		framework.WithSecretsCallback(func(_ []types.Address, tcc *framework.TestClusterConfig) {
			addresses := make([]string, len(accounts)+1)
			for i := 0; i < len(accounts); i++ {
				addresses[i] = accounts[i].Address().String()
			}

			addresses[len(accounts)] = deployerKey.Address().String()

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

	rootERC20Token := contracts.NativeERC20TokenContract

	wg := sync.WaitGroup{}

	for i := range numberOfBridges {
		wg.Add(1)

		go func(bridgeNum int) {
			defer wg.Done()

			logFunc := func(format string, args ...any) {
				pf := fmt.Sprintf("[%s⇄%s] ", internalChainID.String(), externalChainIDs[bridgeNum].String())
				t.Logf(pf+format, args...)
			}

			logFunc("Root native (mintable) token smart contract was successfully deployed on the internal chain %d at address %s", internalChainID, rootERC20Token.String())

			balances := make([]*big.Int, numberOfAccounts)

			logFunc("Balances of accounts on internal chain before deposit:")
			for i := 0; i < numberOfAccounts; i++ {
				balance, err := internalChainTxRelayer.Client().GetBalance(accounts[i].Address(), jsonrpc.LatestBlockNumberOrHash)
				require.NoError(t, err)

				logFunc("#%d - %s : %s Wei", i+1, accounts[i].Address().String(), balance.String())
				balances[i] = balance
			}

			for i := 0; i < numberOfAccounts; i++ {
				rawKey, err := accounts[i].MarshallPrivateKey()
				require.NoError(t, err)

				err = cluster.Bridges[bridgeNum].Deposit(
					common.ERC20,
					rootERC20Token,
					bridgeConfigs[bridgeNum].InternalMintableERC20PredicateAddr,
					hex.EncodeToString(rawKey),
					accounts[i].Address().String(),
					"100000000000000000",
					"",
					cluster.Servers[0].JSONRPCAddr(),
					"",
					true,
				)

				require.NoError(t, err)

				logFunc("The deposit was made for the account %s", accounts[i].Address().String())
			}

			logFunc("Balances of accounts on internal chain after deposit:")
			for i := 0; i < numberOfAccounts; i++ {
				balance, err := internalChainTxRelayer.Client().GetBalance(accounts[i].Address(), jsonrpc.LatestBlockNumberOrHash)
				require.NoError(t, err)

				logFunc("#%d - %s : %s Wei", i+1, accounts[i].Address().String(), balance.String())
			}

			require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
				for i := range numberOfAccounts + 1 {
					if !isEventProcessed(t, bridgeConfigs[bridgeNum].ExternalGatewayAddr, externalChainTxRelayers[bridgeNum], uint64(i+1)) {
						logFunc("Event %d still not processed", i+1)
						return false
					}
				}

				logFunc("All events are successfully processed")
				return true
			}))

			childERC20Token := getChildToken(t, contractsapi.RootERC20Predicate.Abi,
				bridgeConfigs[bridgeNum].InternalMintableERC20PredicateAddr, rootERC20Token, internalChainTxRelayer)

			logFunc("Child ERC20 smart contract was successfully deployed on the external chain at address %s", childERC20Token.String())

			for _, account := range accounts {
				balance := erc20BalanceOf(t, account.Address(), childERC20Token, externalChainTxRelayers[bridgeNum])
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
					cluster.Bridges[bridgeNum].JSONRPCAddr(),
					bridgeConfigs[bridgeNum].ExternalMintableERC20PredicateAddr,
					childERC20Token,
					false)
				require.NoError(t, err)

				logFunc("The withdraw was made for the account %s", accounts[i].Address().String())
			}

			require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
				for i := range numberOfAccounts {
					if !isEventProcessed(t, bridgeConfigs[bridgeNum].InternalGatewayAddr, internalChainTxRelayer, uint64(i+1)) {
						logFunc("Event %d still not processed", i+1)
						return false
					}
				}

				logFunc("All events are successfully processed")
				return true
			}))

			logFunc("Balances of accounts on internal chain after withdraw:")
			for i := 0; i < numberOfAccounts; i++ {
				balance, err := internalChainTxRelayer.Client().GetBalance(accounts[i].Address(), jsonrpc.LatestBlockNumberOrHash)
				require.NoError(t, err)

				logFunc("#%d - %s : %s Wei", i+1, accounts[i].Address().String(), balance.String())
				require.Equal(t, balances[i], balance)
			}
		}(i)
	}
	wg.Wait()
}
