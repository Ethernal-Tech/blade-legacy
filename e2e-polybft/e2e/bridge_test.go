package e2e

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/Ethernal-Tech/ethgo"
	"github.com/stretchr/testify/require"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/bridge/common"
	bridgeHelper "github.com/0xPolygon/polygon-edge/command/bridge/helper"
	validatorHelper "github.com/0xPolygon/polygon-edge/command/validator/helper"
	polycfg "github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	helperCommon "github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/state/runtime/addresslist"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	chainConfigFileName = "genesis.json"
)

func TestE2E_Bridge_ExternalChainTokensTransfers(t *testing.T) {
	const (
		transfersCount        = 5
		numBlockConfirmations = 2
		// make epoch size long enough, so that all exit events are processed within the same epoch
		epochSize             = 40
		sprintSize            = uint64(5)
		numberOfAttempts      = 7
		stateSyncedLogsCount  = 2 // map token and deposit
		numberOfBridges       = 1
		numberOfMapTokenEvent = 1
	)

	var (
		bridgeAmount        = ethgo.Ether(2)
		bridgeMessageResult contractsapi.BridgeMessageResultEvent
	)

	receiversAddrs := make([]types.Address, transfersCount)
	receivers := make([]string, transfersCount)
	amounts := make([]string, transfersCount)
	receiverKeys := make([]string, transfersCount)

	for i := 0; i < transfersCount; i++ {
		key, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		rawKey, err := key.MarshallPrivateKey()
		require.NoError(t, err)

		receiverKeys[i] = hex.EncodeToString(rawKey)
		receiversAddrs[i] = key.Address()
		receivers[i] = key.Address().String()
		amounts[i] = fmt.Sprintf("%d", bridgeAmount)

		t.Logf("Receiver#%d=%s\n", i+1, receivers[i])
	}

	cluster := framework.NewTestCluster(t, 5,
		framework.WithTestRewardToken(),
		framework.WithNumBlockConfirmations(numBlockConfirmations),
		framework.WithEpochSize(epochSize),
		framework.WithBridges(numberOfBridges),
		framework.WithSecretsCallback(func(addrs []types.Address, tcc *framework.TestClusterConfig) {
			for i := 0; i < len(addrs); i++ {
				// premine receivers, so that they are able to do withdrawals
				tcc.StakeAmounts = append(tcc.StakeAmounts, ethgo.Ether(10))
			}

			tcc.Premine = append(tcc.Premine, receivers...)
		}))

	defer cluster.Stop()

	bridgeOne := 0

	cluster.WaitForReady(t)

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	validatorSrv := cluster.Servers[0]

	childEthEndpoint := validatorSrv.JSONRPC()

	externalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[0].JSONRPCAddr()))
	require.NoError(t, err)

	chainID, err := externalChainTxRelayer.Client().ChainID()
	require.NoError(t, err)

	bridgeCfg := polybftCfg.Bridge[chainID.Uint64()]

	txRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(childEthEndpoint))
	require.NoError(t, err)

	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	deployTx := types.NewTx(types.NewLegacyTx(
		types.WithTo(nil),
		types.WithInput(contractsapi.RootERC20.Bytecode),
	))

	receipt, err := externalChainTxRelayer.SendTransaction(deployTx, deployerKey)
	require.NoError(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

	rootERC20Token := types.Address(receipt.ContractAddress)
	t.Log("External chain token address:", rootERC20Token)

	// wait for a couple of sprints
	finalBlockNum := 1 * sprintSize
	require.NoError(t, cluster.WaitForBlock(finalBlockNum, 2*time.Minute))

	t.Run("bridge ERC20 tokens", func(t *testing.T) {
		// DEPOSIT ERC20 TOKENS
		// send a few transactions to the bridge
		require.NoError(t,
			cluster.Bridges[bridgeOne].Deposit(
				common.ERC20,
				rootERC20Token,
				bridgeCfg.ExternalERC20PredicateAddr,
				bridgeHelper.TestAccountPrivKey,
				strings.Join(receivers, ","),
				strings.Join(amounts, ","),
				"",
				cluster.Bridges[bridgeOne].JSONRPCAddr(),
				bridgeHelper.TestAccountPrivKey,
				false,
			))

		finalBlockNum := 10 * sprintSize
		// wait for a couple of sprints
		require.NoError(t, cluster.WaitForBlock(finalBlockNum, 2*time.Minute))

		// the bridge transactions are processed and there should be a success state sync events
		logs, err := getFilteredLogs(bridgeMessageResult.Sig(), 0, finalBlockNum, childEthEndpoint)
		require.NoError(t, err)

		// assert that all deposits are executed successfully
		// because of the token mapping with the first deposit
		assertBridgeEventResultSuccess(t, logs, transfersCount+1)

		// get child token address
		childERC20Token := getChildToken(t, contractsapi.RootERC20Predicate.Abi,
			bridgeCfg.ExternalERC20PredicateAddr, rootERC20Token, externalChainTxRelayer)

		// check receivers balances got increased by deposited amount
		for _, receiver := range receivers {
			balance := erc20BalanceOf(t, types.StringToAddress(receiver), childERC20Token, txRelayer)
			require.Equal(t, bridgeAmount, balance)
		}

		t.Log("Deposits were successfully processed")

		require.NoError(t, cluster.WaitForBlock(uint64(epochSize), 2*time.Minute))

		// WITHDRAW ERC20 TOKENS
		// send withdraw transaction
		for i, senderKey := range receiverKeys {
			err = cluster.Bridges[bridgeOne].Withdraw(
				common.ERC20,
				senderKey,
				receivers[i],
				amounts[i],
				"",
				validatorSrv.JSONRPCAddr(),
				bridgeCfg.InternalERC20PredicateAddr,
				childERC20Token,
				false)
			require.NoError(t, err)
		}

		require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
			for i := range receivers {
				if !isEventProcessed(t, bridgeCfg.ExternalGatewayAddr, externalChainTxRelayer, uint64(i+1)) {
					return false
				}
			}

			return true
		}))

		for _, receiver := range receivers {
			// assert that receiver's balance on RootERC20 smart contract is as expected
			balance := erc20BalanceOf(t, types.StringToAddress(receiver), rootERC20Token, externalChainTxRelayer)
			require.True(t, bridgeAmount.Cmp(balance) == 0)
		}
	})

	t.Run("multiple deposit batches per epoch", func(t *testing.T) {
		const (
			depositsSubset = 1
		)

		internalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(childEthEndpoint))
		require.NoError(t, err)

		lastCommittedIDMethod := contractsapi.BridgeStorage.Abi.GetMethod("lastCommitted")
		lastCommittedIDInput, err := lastCommittedIDMethod.Encode([]interface{}{chainID.Uint64()})
		require.NoError(t, err)

		// check for last committed id
		commitmentIDRaw, err := internalChainTxRelayer.Call(types.ZeroAddress, contracts.BridgeStorageContract, lastCommittedIDInput)
		require.NoError(t, err)

		_, err = helperCommon.ParseUint64orHex(&commitmentIDRaw)
		require.NoError(t, err)

		initialBlockNum, err := childEthEndpoint.BlockNumber()
		require.NoError(t, err)

		// wait for next sprint block as the starting point,
		// in order to be able to make assertions against blocks offsetted by sprints
		initialBlockNum = initialBlockNum + sprintSize - (initialBlockNum % sprintSize)
		require.NoError(t, cluster.WaitForBlock(initialBlockNum, 1*time.Minute))

		// send two transactions to the bridge so that we have a minimal batch
		require.NoError(t, cluster.Bridges[bridgeOne].Deposit(
			common.ERC20,
			rootERC20Token,
			bridgeCfg.ExternalERC20PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join(receivers[:depositsSubset], ","),
			strings.Join(amounts[:depositsSubset], ","),
			"",
			cluster.Bridges[bridgeOne].JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
		)

		// wait for a few more sprints
		midBlockNumber := initialBlockNum + 2*sprintSize
		require.NoError(t, cluster.WaitForBlock(midBlockNumber, 2*time.Minute))

		require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
			for i := range receivers[:depositsSubset] {
				if !isEventProcessed(t,
					bridgeCfg.InternalGatewayAddr,
					internalChainTxRelayer,
					// this sum represent minimal value for event id based on earlier events
					uint64(numberOfMapTokenEvent+transfersCount+depositsSubset+i)) {
					return false
				}
			}

			return true
		}))

		// send some more transactions to the bridge to build another commitment in epoch
		require.NoError(t, cluster.Bridges[bridgeOne].Deposit(
			common.ERC20,
			rootERC20Token,
			bridgeCfg.ExternalERC20PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join(receivers[depositsSubset:], ","),
			strings.Join(amounts[depositsSubset:], ","),
			"",
			cluster.Bridges[bridgeOne].JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
		)

		finalBlockNum := midBlockNumber + 5*sprintSize
		// wait for a few more sprints
		require.NoError(t, cluster.WaitForBlock(midBlockNumber+5*sprintSize, 3*time.Minute))

		require.NoError(t, cluster.WaitUntil(time.Minute*2, time.Second*2, func() bool {
			for i := range receivers[depositsSubset:] {
				if !isEventProcessed(t,
					bridgeCfg.InternalGatewayAddr,
					internalChainTxRelayer,
					// this sum represent minimal value for event id based on earlier events
					uint64(numberOfMapTokenEvent+transfersCount+2*depositsSubset+i)) {
					return false
				}
			}

			return true
		}))

		// the transactions are mined and state syncs should be executed by the relayer
		// and there should be a success events
		logs, err := getFilteredLogs(bridgeMessageResult.Sig(), initialBlockNum, finalBlockNum, childEthEndpoint)
		require.NoError(t, err)

		// assert that all state syncs are executed successfully
		assertBridgeEventResultSuccess(t, logs, transfersCount)
	})
}

func TestE2E_Bridge_ERC721Transfer(t *testing.T) {
	const (
		transfersCount       = 4
		epochSize            = 5
		numberOfAttempts     = 4
		stateSyncedLogsCount = 2
		numberOfBridges      = 1
	)

	receiverKeys := make([]string, transfersCount)
	receivers := make([]string, transfersCount)
	receiversAddrs := make([]types.Address, transfersCount)
	tokenIDs := make([]string, transfersCount)

	for i := 0; i < transfersCount; i++ {
		key, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		rawKey, err := key.MarshallPrivateKey()
		require.NoError(t, err)

		receiverKeys[i] = hex.EncodeToString(rawKey)
		receivers[i] = key.Address().String()
		receiversAddrs[i] = key.Address()
		tokenIDs[i] = fmt.Sprintf("%d", i)

		t.Logf("Receiver#%d=%s\n", i+1, receivers[i])
	}

	cluster := framework.NewTestCluster(t, 5,
		framework.WithEpochSize(epochSize),
		framework.WithPremine(receiversAddrs...),
		framework.WithBridges(numberOfBridges),
		framework.WithSecretsCallback(func(addrs []types.Address, tcc *framework.TestClusterConfig) {
			for i := 0; i < len(addrs); i++ {
				tcc.StakeAmounts = append(tcc.StakeAmounts, ethgo.Ether(10))
			}
		}),
	)
	defer cluster.Stop()

	bridgeOne := 0

	cluster.WaitForReady(t)

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	externalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[bridgeOne].JSONRPCAddr()))
	require.NoError(t, err)

	chainID, err := externalChainTxRelayer.Client().ChainID()
	require.NoError(t, err)

	bridgeCfg := polybftCfg.Bridge[chainID.Uint64()]

	externalChainDeployer, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	deployTx := types.NewTx(&types.LegacyTx{
		BaseTx: &types.BaseTx{
			To:    nil,
			Input: contractsapi.RootERC721.Bytecode,
		},
	})

	// deploy root ERC 721 token
	receipt, err := externalChainTxRelayer.SendTransaction(deployTx, externalChainDeployer)
	require.NoError(t, err)

	rootERC721Addr := types.Address(receipt.ContractAddress)

	// DEPOSIT ERC721 TOKENS
	// send a few transactions to the bridge
	require.NoError(
		t,
		cluster.Bridges[bridgeOne].Deposit(
			common.ERC721,
			rootERC721Addr,
			bridgeCfg.ExternalERC721PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join(receivers, ","),
			"",
			strings.Join(tokenIDs, ","),
			cluster.Bridges[bridgeOne].JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
	)

	// wait for a few more sprints
	require.NoError(t, cluster.WaitForBlock(50, 4*time.Minute))

	validatorSrv := cluster.Servers[0]
	childEthEndpoint := validatorSrv.JSONRPC()

	// the transactions are processed and there should be a success events
	var bridgeMessageResult contractsapi.BridgeMessageResultEvent

	for i := 0; i < numberOfAttempts; i++ {
		logs, err := getFilteredLogs(bridgeMessageResult.Sig(), 0, uint64(50+i*epochSize), childEthEndpoint)
		require.NoError(t, err)

		if len(logs) == stateSyncedLogsCount || i == numberOfAttempts-1 {
			// assert that all deposits are executed successfully.
			// All deposits are sent using a single transaction, so arbitrary message bridge emits two state sync events:
			// MAP_TOKEN_SIG and DEPOSIT_BATCH_SIG state sync events
			assertBridgeEventResultSuccess(t, logs, stateSyncedLogsCount)

			break
		}

		require.NoError(t, cluster.WaitForBlock(uint64(50+(i+1)*epochSize), 1*time.Minute))
	}

	txRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(childEthEndpoint))
	require.NoError(t, err)

	// retrieve child token address (from both chains, and assert they are the same)
	externalChildTokenAddr := getChildToken(t, contractsapi.RootERC721Predicate.Abi, bridgeCfg.ExternalERC721PredicateAddr,
		rootERC721Addr, externalChainTxRelayer)
	internalChildTokenAddr := getChildToken(t, contractsapi.ChildERC721Predicate.Abi, bridgeCfg.InternalERC721PredicateAddr,
		rootERC721Addr, txRelayer)

	t.Log("External child token", externalChildTokenAddr)
	t.Log("Internal child token", internalChildTokenAddr)
	require.Equal(t, externalChildTokenAddr, internalChildTokenAddr)

	for i, receiver := range receiversAddrs {
		owner := erc721OwnerOf(t, big.NewInt(int64(i)), internalChildTokenAddr, txRelayer)
		require.Equal(t, receiver, owner)
	}

	t.Log("Deposits were successfully processed")

	// WITHDRAW ERC721 TOKENS
	for i, receiverKey := range receiverKeys {
		// send withdraw transactions
		err = cluster.Bridges[bridgeOne].Withdraw(
			common.ERC721,
			receiverKey,
			receivers[i],
			"",
			tokenIDs[i],
			validatorSrv.JSONRPCAddr(),
			bridgeCfg.InternalERC721PredicateAddr,
			internalChildTokenAddr,
			false)
		require.NoError(t, err)
	}

	currentBlock, err := childEthEndpoint.GetBlockByNumber(jsonrpc.LatestBlockNumber, false)
	require.NoError(t, err)

	currentExtra, err := polytypes.GetIbftExtra(currentBlock.Header.ExtraData)
	require.NoError(t, err)

	t.Logf("Latest block number: %d, epoch number: %d\n", currentBlock.Number(), currentExtra.BlockMetaData.EpochNumber)

	require.NoError(t, cluster.WaitUntil(time.Minute*3, time.Second*2, func() bool {
		for i := 1; i <= transfersCount; i++ {
			if !isEventProcessed(t, bridgeCfg.ExternalGatewayAddr, externalChainTxRelayer, uint64(i)) {
				return false
			}
		}

		return true
	}))

	// assert that owners of given token ids are the accounts on the root chain ERC 721 token
	for i, receiver := range receiversAddrs {
		t.Log("ERC721 OWNER", i)
		owner := erc721OwnerOf(t, big.NewInt(int64(i)), rootERC721Addr, externalChainTxRelayer)
		require.Equal(t, receiver, owner)
	}
}

func TestE2E_Bridge_ERC1155Transfer(t *testing.T) {
	const (
		transfersCount       = 5
		amount               = 100
		epochSize            = 5
		numberOfAttempts     = 4
		stateSyncedLogsCount = 2
		numberOfBridges      = 1
	)

	receiverKeys := make([]string, transfersCount)
	receivers := make([]string, transfersCount)
	receiversAddrs := make([]types.Address, transfersCount)
	amounts := make([]string, transfersCount)
	tokenIDs := make([]string, transfersCount)

	for i := 0; i < transfersCount; i++ {
		key, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		rawKey, err := key.MarshallPrivateKey()
		require.NoError(t, err)

		receiverKeys[i] = hex.EncodeToString(rawKey)
		receivers[i] = key.Address().String()
		receiversAddrs[i] = key.Address()
		amounts[i] = fmt.Sprintf("%d", amount)
		tokenIDs[i] = fmt.Sprintf("%d", i+1)

		t.Logf("Receiver#%d=%s\n", i+1, receivers[i])
	}

	cluster := framework.NewTestCluster(t, 5,
		framework.WithNumBlockConfirmations(0),
		framework.WithEpochSize(epochSize),
		framework.WithPremine(receiversAddrs...),
		framework.WithBridges(numberOfBridges),
		framework.WithSecretsCallback(func(addrs []types.Address, tcc *framework.TestClusterConfig) {
			for i := 0; i < len(addrs); i++ {
				tcc.StakeAmounts = append(tcc.StakeAmounts, ethgo.Ether(10))
			}
		}),
	)
	defer cluster.Stop()

	bridgeOne := 0

	cluster.WaitForReady(t)

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	externalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[bridgeOne].JSONRPCAddr()))
	require.NoError(t, err)

	chainID, err := externalChainTxRelayer.Client().ChainID()
	require.NoError(t, err)

	bridgeCfg := polybftCfg.Bridge[chainID.Uint64()]

	externalChainDeployer, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	deployTx := types.NewTx(&types.LegacyTx{
		BaseTx: &types.BaseTx{
			To:    nil,
			Input: contractsapi.RootERC1155.Bytecode,
		},
	})

	// deploy root ERC 1155 token
	receipt, err := externalChainTxRelayer.SendTransaction(deployTx, externalChainDeployer)
	require.NoError(t, err)

	rootERC1155Addr := types.Address(receipt.ContractAddress)

	// DEPOSIT ERC1155 TOKENS
	// send a few transactions to the bridge
	require.NoError(
		t,
		cluster.Bridges[bridgeOne].Deposit(
			common.ERC1155,
			rootERC1155Addr,
			bridgeCfg.ExternalERC1155PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join(receivers, ","),
			strings.Join(amounts, ","),
			strings.Join(tokenIDs, ","),
			cluster.Bridges[bridgeOne].JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
	)

	// wait for a few more sprints
	require.NoError(t, cluster.WaitForBlock(50, 4*time.Minute))

	validatorSrv := cluster.Servers[0]
	childEthEndpoint := validatorSrv.JSONRPC()

	// the transactions are processed and there should be a success events
	var bridgeMessageResult contractsapi.BridgeMessageResultEvent

	for i := 0; i < numberOfAttempts; i++ {
		logs, err := getFilteredLogs(bridgeMessageResult.Sig(), 0, uint64(50+i*epochSize), childEthEndpoint)
		require.NoError(t, err)

		if len(logs) == stateSyncedLogsCount || i == numberOfAttempts-1 {
			// assert that all deposits are executed successfully.
			// All deposits are sent using a single transaction, so arbitrary message bridge emits two state sync events:
			// MAP_TOKEN_SIG and DEPOSIT_BATCH_SIG state sync events
			assertBridgeEventResultSuccess(t, logs, stateSyncedLogsCount)

			break
		}

		require.NoError(t, cluster.WaitForBlock(uint64(50+(i+1)*epochSize), 1*time.Minute))
	}

	txRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(childEthEndpoint))
	require.NoError(t, err)

	// retrieve child token address
	l1ChildTokenAddr := getChildToken(t, contractsapi.RootERC1155Predicate.Abi, bridgeCfg.ExternalERC1155PredicateAddr,
		rootERC1155Addr, externalChainTxRelayer)
	l2ChildTokenAddr := getChildToken(t, contractsapi.ChildERC1155Predicate.Abi, bridgeCfg.InternalERC1155PredicateAddr,
		rootERC1155Addr, txRelayer)

	t.Log("L1 child token", l1ChildTokenAddr)
	t.Log("L2 child token", l2ChildTokenAddr)
	require.Equal(t, l1ChildTokenAddr, l2ChildTokenAddr)

	// check receivers balances got increased by deposited amount
	for i, receiver := range receivers {
		balanceOfFn := &contractsapi.BalanceOfChildERC1155Fn{
			Account: types.StringToAddress(receiver),
			ID:      big.NewInt(int64(i + 1)),
		}

		balanceInput, err := balanceOfFn.EncodeAbi()
		require.NoError(t, err)

		balanceRaw, err := txRelayer.Call(types.ZeroAddress, l2ChildTokenAddr, balanceInput)
		require.NoError(t, err)

		balance, err := helperCommon.ParseUint256orHex(&balanceRaw)
		require.NoError(t, err)
		require.Equal(t, big.NewInt(int64(amount)), balance)
	}

	t.Log("Deposits were successfully processed")

	// WITHDRAW ERC1155 TOKENS
	senderAccount, err := validatorHelper.GetAccountFromDir(cluster.Servers[0].DataDir())
	require.NoError(t, err)

	t.Logf("Withdraw sender: %s\n", senderAccount.Ecdsa.Address())

	for i, receiverKey := range receiverKeys {
		// send withdraw transactions
		err = cluster.Bridges[bridgeOne].Withdraw(
			common.ERC1155,
			receiverKey,
			receivers[i],
			amounts[i],
			tokenIDs[i],
			validatorSrv.JSONRPCAddr(),
			bridgeCfg.InternalERC1155PredicateAddr,
			l2ChildTokenAddr,
			false)
		require.NoError(t, err)
	}

	require.NoError(t, cluster.WaitUntil(time.Minute*3, time.Second*2, func() bool {
		for i := 1; i <= transfersCount; i++ {
			if !isEventProcessed(t, bridgeCfg.ExternalGatewayAddr, externalChainTxRelayer, uint64(i)) {
				return false
			}
		}

		return true
	}))

	// assert that receiver's balances on RootERC1155 smart contract are expected
	for i, receiver := range receivers {
		balanceOfFn := &contractsapi.BalanceOfRootERC1155Fn{
			Account: types.StringToAddress(receiver),
			ID:      big.NewInt(int64(i + 1)),
		}

		balanceInput, err := balanceOfFn.EncodeAbi()
		require.NoError(t, err)

		balanceRaw, err := externalChainTxRelayer.Call(types.ZeroAddress, rootERC1155Addr, balanceInput)
		require.NoError(t, err)

		balance, err := helperCommon.ParseUint256orHex(&balanceRaw)
		require.NoError(t, err)
		require.Equal(t, big.NewInt(amount), balance)
	}
}

func TestE2E_Bridge_InternalChainTokensTransfer(t *testing.T) {
	const (
		transfersCount = uint64(4)
		amount         = 100
		// make epoch size long enough, so that all exit events are processed within the same epoch
		epochSize        = 30
		sprintSize       = uint64(5)
		numberOfAttempts = 4
		numberOfBridges  = 1
	)

	// init private keys and amounts
	depositorKeys := make([]string, transfersCount)
	depositors := make([]types.Address, transfersCount)
	amounts := make([]string, transfersCount)
	funds := make([]*big.Int, transfersCount)
	singleToken := ethgo.Ether(1)

	admin, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	adminAddr := admin.Address()

	for i := uint64(0); i < transfersCount; i++ {
		key, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		rawKey, err := key.MarshallPrivateKey()
		require.NoError(t, err)

		depositorKeys[i] = hex.EncodeToString(rawKey)
		depositors[i] = key.Address()
		funds[i] = singleToken
		amounts[i] = fmt.Sprintf("%d", amount)

		t.Logf("Depositor#%d=%s\n", i+1, depositors[i])
	}

	// setup cluster
	cluster := framework.NewTestCluster(t, 5,
		framework.WithNumBlockConfirmations(0),
		framework.WithEpochSize(epochSize),
		framework.WithBridges(numberOfBridges),
		framework.WithBridgeBlockListAdmin(adminAddr),
		framework.WithPremine(append(depositors, adminAddr)...)) //nolint:makezero
	defer cluster.Stop()

	bridgeOne := 0

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	validatorSrv := cluster.Servers[0]
	childEthEndpoint := validatorSrv.JSONRPC()

	// fund accounts on external
	require.NoError(t, validatorSrv.ExternalChainFundFor(depositors, funds, uint64(bridgeOne)))

	cluster.WaitForReady(t)

	externalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[bridgeOne].JSONRPCAddr()))
	require.NoError(t, err)

	chainID, err := externalChainTxRelayer.Client().ChainID()
	require.NoError(t, err)

	bridgeCfg := polybftCfg.Bridge[chainID.Uint64()]

	internalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(childEthEndpoint))
	require.NoError(t, err)

	t.Run("bridge native tokens", func(t *testing.T) {
		// rootToken represents deposit token (basically native mintable token from the Supernets)
		rootToken := contracts.NativeERC20TokenContract

		// block list first depositor
		setAccessListRole(t, cluster, contracts.BlockListBridgeAddr, depositors[0], addresslist.EnabledRole, admin)

		// try sending a single native token deposit transaction
		// it should fail, because first depositor is added to bridge transactions block list
		err = cluster.Bridges[bridgeOne].Deposit(
			common.ERC20,
			rootToken,
			bridgeCfg.InternalMintableERC20PredicateAddr,
			depositorKeys[0],
			depositors[0].String(),
			amounts[0],
			"",
			validatorSrv.JSONRPCAddr(),
			"",
			true)
		require.Error(t, err)

		// remove first depositor from bridge transactions block list
		setAccessListRole(t, cluster, contracts.BlockListBridgeAddr, depositors[0], addresslist.NoRole, admin)

		// allow list each depositor and make sure deposit is successfully executed
		for i, key := range depositorKeys {
			// make sure deposit is successfully executed
			err = cluster.Bridges[bridgeOne].Deposit(
				common.ERC20,
				rootToken,
				bridgeCfg.InternalMintableERC20PredicateAddr,
				key,
				depositors[i].String(),
				amounts[i],
				"",
				validatorSrv.JSONRPCAddr(),
				"",
				true)
			require.NoError(t, err)
		}

		// first exit event is mapping child token on a rootchain
		require.NoError(t, cluster.WaitUntil(time.Minute*3, time.Second*2, func() bool {
			for i := uint64(1); i <= transfersCount+1; i++ {
				if !isEventProcessed(t, bridgeCfg.ExternalGatewayAddr, externalChainTxRelayer, i) {
					return false
				}
			}

			return true
		}))

		// retrieve child mintable token address from both chains and make sure they are the same
		l1ChildToken := getChildToken(t, contractsapi.ChildERC20Predicate.Abi, bridgeCfg.ExternalMintableERC20PredicateAddr,
			rootToken, externalChainTxRelayer)
		l2ChildToken := getChildToken(t, contractsapi.RootERC20Predicate.Abi, bridgeCfg.InternalMintableERC20PredicateAddr,
			rootToken, internalChainTxRelayer)

		t.Log("L1 child token", l1ChildToken)
		t.Log("L2 child token", l2ChildToken)
		require.Equal(t, l1ChildToken, l2ChildToken)

		// check that balances on external chain have increased by deposited amounts
		for _, depositor := range depositors {
			balance := erc20BalanceOf(t, depositor, l1ChildToken, externalChainTxRelayer)
			require.Equal(t, big.NewInt(amount), balance)
		}

		balancesBefore := make([]*big.Int, transfersCount)
		for i := uint64(0); i < transfersCount; i++ {
			balancesBefore[i], err = childEthEndpoint.GetBalance(depositors[i], jsonrpc.LatestBlockNumberOrHash)
			require.NoError(t, err)
		}

		// withdraw child token on the external chain
		for i, depositorKey := range depositorKeys {
			err = cluster.Bridges[bridgeOne].Withdraw(
				common.ERC20,
				depositorKey,
				depositors[i].String(),
				amounts[i],
				"",
				cluster.Bridges[bridgeOne].JSONRPCAddr(),
				bridgeCfg.ExternalMintableERC20PredicateAddr,
				l1ChildToken,
				true)
			require.NoError(t, err)
		}

		allSuccessful := false

		for it := 0; it < numberOfAttempts && !allSuccessful; it++ {
			blockNum, err := childEthEndpoint.BlockNumber()
			require.NoError(t, err)

			// wait a couple of sprints to finalize state sync events
			require.NoError(t, cluster.WaitForBlock(blockNum+3*sprintSize, 2*time.Minute))

			allSuccessful = true

			// check that balances on the child chain are correct
			for i, receiver := range depositors {
				balance := erc20BalanceOf(t, receiver, contracts.NativeERC20TokenContract, internalChainTxRelayer)
				t.Log("Attempt", it+1, "Balance before", balancesBefore[i], "Balance after", balance)

				if balance.Cmp(new(big.Int).Add(balancesBefore[i], big.NewInt(amount))) != 0 {
					allSuccessful = false

					break
				}
			}
		}

		require.True(t, allSuccessful)
	})

	t.Run("bridge ERC 721 tokens", func(t *testing.T) {
		erc721DeployTxn := cluster.Deploy(t, admin, contractsapi.RootERC721.Bytecode)
		require.True(t, erc721DeployTxn.Succeed())
		rootERC721Token := types.Address(erc721DeployTxn.Receipt().ContractAddress)

		for _, depositor := range depositors {
			// mint all the depositors in advance
			mintFn := &contractsapi.MintRootERC721Fn{To: depositor}
			mintInput, err := mintFn.EncodeAbi()
			require.NoError(t, err)

			mintTxn := cluster.MethodTxn(t, admin, rootERC721Token, mintInput)
			require.True(t, mintTxn.Succeed())

			// add all depositors to bride block list
			setAccessListRole(t, cluster, contracts.BlockListBridgeAddr, depositor, addresslist.EnabledRole, admin)
		}

		// deposit should fail because depositors are in bridge block list
		err = cluster.Bridges[bridgeOne].Deposit(
			common.ERC721,
			rootERC721Token,
			bridgeCfg.InternalMintableERC721PredicateAddr,
			depositorKeys[0],
			depositors[0].String(),
			"",
			fmt.Sprintf("%d", 0),
			validatorSrv.JSONRPCAddr(),
			"",
			true)
		require.Error(t, err)

		for i, depositorKey := range depositorKeys {
			// remove all depositors from the bridge block list
			setAccessListRole(t, cluster, contracts.BlockListBridgeAddr, depositors[i], addresslist.NoRole, admin)

			// deposit (without minting, as it was already done beforehand)
			err = cluster.Bridges[bridgeOne].Deposit(
				common.ERC721,
				rootERC721Token,
				bridgeCfg.InternalMintableERC721PredicateAddr,
				depositorKey,
				depositors[i].String(),
				"",
				fmt.Sprintf("%d", i),
				validatorSrv.JSONRPCAddr(),
				"",
				true)
			require.NoError(t, err)
		}

		// first exit event is mapping child token on a rootchain
		require.NoError(t, cluster.WaitUntil(time.Minute*3, time.Second*2, func() bool {
			for i := uint64(1); i <= transfersCount+1; i++ {
				if !isEventProcessed(t, bridgeCfg.ExternalGatewayAddr, externalChainTxRelayer, i) {
					return false
				}
			}

			return true
		}))

		// retrieve child token addresses on both chains and make sure they are the same
		l1ChildToken := getChildToken(t, contractsapi.ChildERC721Predicate.Abi, bridgeCfg.ExternalMintableERC721PredicateAddr,
			rootERC721Token, externalChainTxRelayer)
		l2ChildToken := getChildToken(t, contractsapi.RootERC721Predicate.Abi, bridgeCfg.InternalMintableERC721PredicateAddr,
			rootERC721Token, internalChainTxRelayer)

		t.Log("L1 child token", l1ChildToken)
		t.Log("L2 child token", l2ChildToken)
		require.Equal(t, l1ChildToken, l2ChildToken)

		blockNumber, err := childEthEndpoint.BlockNumber()
		require.NoError(t, err)

		require.NoError(t, cluster.WaitForBlock(blockNumber+epochSize, 2*time.Minute))

		// check owner on the external chain
		for i := uint64(0); i < transfersCount; i++ {
			owner := erc721OwnerOf(t, new(big.Int).SetUint64(i), l1ChildToken, externalChainTxRelayer)
			t.Log("ChildERC721 owner", owner)
			require.Equal(t, depositors[i], owner)
		}

		// withdraw tokens
		for i, depositorKey := range depositorKeys {
			err = cluster.Bridges[bridgeOne].Withdraw(
				common.ERC721,
				depositorKey,
				depositors[i].String(),
				"",
				fmt.Sprintf("%d", i),
				cluster.Bridges[bridgeOne].JSONRPCAddr(),
				bridgeCfg.ExternalMintableERC721PredicateAddr,
				l1ChildToken,
				true)
			require.NoError(t, err)
		}

		allSuccessful := false

		for it := 0; it < numberOfAttempts && !allSuccessful; it++ {
			internalChainBlockNum, err := childEthEndpoint.BlockNumber()
			require.NoError(t, err)

			// wait for commitment execution
			require.NoError(t, cluster.WaitForBlock(internalChainBlockNum+3*sprintSize, 2*time.Minute))

			allSuccessful = true
			// check owners on the child chain
			for i, receiver := range depositors {
				owner := erc721OwnerOf(t, big.NewInt(int64(i)), rootERC721Token, internalChainTxRelayer)
				t.Log("Attempt:", it+1, " Owner:", owner, " Receiver:", receiver)

				if receiver != owner {
					allSuccessful = false

					break
				}
			}
		}

		require.True(t, allSuccessful)
	})
}

func TestE2E_Bridge_Transfers_AccessLists(t *testing.T) {
	var (
		transfersCount = 5
		depositAmount  = ethgo.Ether(5)
		withdrawAmount = ethgo.Ether(1)
		// make epoch size long enough, so that all exit events are processed within the same epoch
		epochSize       = 40
		sprintSize      = uint64(5)
		numberOfBridges = uint64(1)
	)

	receivers := make([]string, transfersCount)
	depositAmounts := make([]string, transfersCount)
	withdrawAmounts := make([]string, transfersCount)

	admin, _ := crypto.GenerateECDSAKey()
	adminAddr := admin.Address()

	cluster := framework.NewTestCluster(t, 5,
		framework.WithNumBlockConfirmations(0),
		framework.WithEpochSize(epochSize),
		framework.WithTestRewardToken(),
		framework.WithRootTrackerPollInterval(3*time.Second),
		framework.WithBridges(numberOfBridges),
		framework.WithBridgeAllowListAdmin(adminAddr),
		framework.WithBridgeBlockListAdmin(adminAddr),
		framework.WithSecretsCallback(func(a []types.Address, tcc *framework.TestClusterConfig) {
			for i := 0; i < len(a); i++ {
				receivers[i] = a[i].String()
				depositAmounts[i] = fmt.Sprintf("%d", depositAmount)
				withdrawAmounts[i] = fmt.Sprintf("%d", withdrawAmount)

				t.Logf("Receiver#%d=%s\n", i+1, receivers[i])

				// premine access list admin account
				tcc.Premine = append(tcc.Premine, adminAddr.String())
				tcc.StakeAmounts = append(tcc.StakeAmounts, ethgo.Ether(10))
			}
		}),
	)
	defer cluster.Stop()

	bridgeOne := 0

	cluster.WaitForReady(t)

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	validatorSrv := cluster.Servers[0]
	childEthEndpoint := validatorSrv.JSONRPC()
	relayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(childEthEndpoint))
	require.NoError(t, err)

	senderAccount, err := validatorHelper.GetAccountFromDir(validatorSrv.DataDir())
	require.NoError(t, err)

	// fund admin on external chain
	require.NoError(t, validatorSrv.ExternalChainFundFor([]types.Address{adminAddr}, []*big.Int{ethgo.Ether(1)}, uint64(bridgeOne)))

	externalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[bridgeOne].JSONRPCAddr()))
	require.NoError(t, err)

	chainID, err := externalChainTxRelayer.Client().ChainID()
	require.NoError(t, err)

	bridgeCfg := polybftCfg.Bridge[chainID.Uint64()]

	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	deployTx := types.NewTx(types.NewLegacyTx(
		types.WithTo(nil),
		types.WithInput(contractsapi.RootERC20.Bytecode),
	))

	// deploy root erc20 token
	receipt, err := externalChainTxRelayer.SendTransaction(deployTx, deployerKey)
	require.NoError(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)
	rootERC20Token := types.Address(receipt.ContractAddress)

	t.Run("bridge ERC 20 tokens", func(t *testing.T) {
		// DEPOSIT ERC20 TOKENS
		// send a few transactions to the bridge
		require.NoError(
			t,
			cluster.Bridges[bridgeOne].Deposit(
				common.ERC20,
				rootERC20Token,
				bridgeCfg.ExternalERC20PredicateAddr,
				bridgeHelper.TestAccountPrivKey,
				strings.Join(receivers, ","),
				strings.Join(depositAmounts, ","),
				"",
				cluster.Bridges[bridgeOne].JSONRPCAddr(),
				bridgeHelper.TestAccountPrivKey,
				false),
		)

		finalBlockNum := 10 * sprintSize
		// wait for a couple of sprints
		require.NoError(t, cluster.WaitForBlock(finalBlockNum, 2*time.Minute))

		var bridgeMessageResult contractsapi.BridgeMessageResultEvent

		// the transactions are processed and there should be a success events
		logs, err := getFilteredLogs(bridgeMessageResult.Sig(), 0, finalBlockNum, childEthEndpoint)
		require.NoError(t, err)

		// assert that all deposits are executed successfully
		// (token mapping and transferCount of deposits)
		assertBridgeEventResultSuccess(t, logs, transfersCount+1)

		// get child token address
		childERC20Token := getChildToken(t, contractsapi.RootERC20Predicate.Abi,
			bridgeCfg.ExternalERC20PredicateAddr, rootERC20Token, externalChainTxRelayer)

		for _, receiver := range receivers {
			balance := erc20BalanceOf(t, types.StringToAddress(receiver), childERC20Token, relayer)
			require.Equal(t, depositAmount, balance)
		}

		t.Log("Deposits were successfully processed")

		oldBalances := map[types.Address]*big.Int{}

		for _, receiver := range receivers {
			balance := erc20BalanceOf(t, types.StringToAddress(receiver), rootERC20Token, externalChainTxRelayer)
			oldBalances[types.StringToAddress(receiver)] = balance
		}

		// WITHDRAW ERC20 TOKENS
		rawKey, err := senderAccount.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		// send withdraw transaction.
		// It should fail because sender is not allow-listed.
		err = cluster.Bridges[bridgeOne].Withdraw(
			common.ERC20,
			hex.EncodeToString(rawKey),
			strings.Join(receivers, ","),
			strings.Join(withdrawAmounts, ","),
			"",
			validatorSrv.JSONRPCAddr(),
			bridgeCfg.InternalERC20PredicateAddr,
			childERC20Token,
			false)
		require.Error(t, err)

		// add account to bridge allow list
		setAccessListRole(t, cluster, contracts.AllowListBridgeAddr, senderAccount.Address(), addresslist.EnabledRole, admin)

		// try to withdraw again
		err = cluster.Bridges[bridgeOne].Withdraw(
			common.ERC20,
			hex.EncodeToString(rawKey),
			strings.Join(receivers, ","),
			strings.Join(withdrawAmounts, ","),
			"",
			validatorSrv.JSONRPCAddr(),
			bridgeCfg.InternalERC20PredicateAddr,
			childERC20Token,
			false)
		require.NoError(t, err)

		// add account to bridge block list
		setAccessListRole(t, cluster, contracts.BlockListBridgeAddr, senderAccount.Address(), addresslist.EnabledRole, admin)

		// it should fail now because sender accont is in the block list
		err = cluster.Bridges[bridgeOne].Withdraw(
			common.ERC20,
			hex.EncodeToString(rawKey),
			strings.Join(receivers, ","),
			strings.Join(withdrawAmounts, ","),
			"",
			validatorSrv.JSONRPCAddr(),
			bridgeCfg.InternalERC20PredicateAddr,
			contracts.NativeERC20TokenContract,
			false)
		require.ErrorContains(t, err, "failed to send withdraw transaction")

		currentBlock, err := childEthEndpoint.GetBlockByNumber(jsonrpc.LatestBlockNumber, false)
		require.NoError(t, err)

		currentExtra, err := polytypes.GetIbftExtra(currentBlock.Header.ExtraData)
		require.NoError(t, err)

		t.Logf("Latest block number: %d, epoch number: %d\n", currentBlock.Number(), currentExtra.BlockMetaData.EpochNumber)

		require.NoError(t, cluster.WaitUntil(time.Minute*3, time.Second*2, func() bool {
			for i := uint64(1); i <= uint64(transfersCount); i++ {
				if !isEventProcessed(t, bridgeCfg.ExternalGatewayAddr, externalChainTxRelayer, i) {
					return false
				}
			}

			return true
		}))

		// assert that receiver's balances on RootERC20 smart contract are expected
		for _, receiver := range receivers {
			balance := erc20BalanceOf(t, types.StringToAddress(receiver), rootERC20Token, externalChainTxRelayer)
			require.Equal(t, oldBalances[types.StringToAddress(receiver)].Add(
				oldBalances[types.StringToAddress(receiver)], withdrawAmount), balance)
		}
	})
}

func TestE2E_Bridge_NonMintableERC20Token_WithPremine(t *testing.T) {
	var (
		stateSyncedLogsCount  = 2
		epochSize             = uint64(10)
		numberOfAttempts      = uint64(4)
		numBlockConfirmations = uint64(2)
		bridgeEvents          = uint64(2)
		tokensToTransfer      = ethgo.Gwei(10)
		tenMilionTokens       = ethgo.Ether(10000000)
		bigZero               = big.NewInt(0)
		numberOfBridges       = 1
	)

	nonValidatorKey, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	nonValidatorKeyRaw, err := nonValidatorKey.MarshallPrivateKey()
	require.NoError(t, err)

	rewardWalletKey, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	rewardWalletKeyRaw, err := rewardWalletKey.MarshallPrivateKey()
	require.NoError(t, err)

	// start cluster with default, non-mintable native erc20 root token
	// with london fork enabled
	cluster := framework.NewTestCluster(t, 5,
		framework.WithBridges(uint64(numberOfBridges)),
		framework.WithEpochSize(int(epochSize)),
		framework.WithNumBlockConfirmations(numBlockConfirmations),
		framework.WithNativeTokenConfig(nativeTokenNonMintableConfig),
		// this enables London (EIP-1559) fork
		framework.WithBurnContract(&polycfg.BurnContractInfo{
			BlockNumber: 0,
			Address:     types.StringToAddress("0xBurnContractAddress")}),
		framework.WithSecretsCallback(func(_ []types.Address, tcc *framework.TestClusterConfig) {
			nonValidatorKeyString := hex.EncodeToString(nonValidatorKeyRaw)
			rewardWalletKeyString := hex.EncodeToString(rewardWalletKeyRaw)

			// do premine to a non validator address
			tcc.Premine = append(tcc.Premine,
				fmt.Sprintf("%s:%s:%s",
					nonValidatorKey.Address(),
					new(big.Int).Mul(big.NewInt(10), tenMilionTokens).String(),
					nonValidatorKeyString))

			// do premine to reward wallet address
			tcc.Premine = append(tcc.Premine,
				fmt.Sprintf("%s:%s:%s",
					rewardWalletKey.Address(),
					command.DefaultPremineBalance.String(),
					rewardWalletKeyString))
		}),
	)
	defer cluster.Stop()

	bridgeOne := 0

	externalChainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridges[bridgeOne].JSONRPCAddr()))
	require.NoError(t, err)

	chainID, err := externalChainTxRelayer.Client().ChainID()
	require.NoError(t, err)

	childEthEndpoint := cluster.Servers[0].JSONRPC()

	cluster.WaitForReady(t)

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	bridgeCfg := polybftCfg.Bridge[chainID.Uint64()]

	checkBalancesFn := func(address types.Address, rootExpected, childExpected *big.Int, isValidator bool) {
		offset := ethgo.Gwei(100)
		expectedValue := new(big.Int)

		t.Log("Checking balance of native ERC20 token on root and child", "Address", address,
			"Root expected", rootExpected, "Child Expected", childExpected)

		balance := erc20BalanceOf(t, address,
			bridgeCfg.ExternalNativeERC20Addr, externalChainTxRelayer)
		t.Log("Balance of native ERC20 token on root", balance, "Address", address)
		require.Equal(t, rootExpected, balance)

		balance, err = childEthEndpoint.GetBalance(address, jsonrpc.LatestBlockNumberOrHash)
		require.NoError(t, err)
		t.Log("Balance of native ERC20 token on child", balance, "Address", address)

		if isValidator {
			require.True(t, balance.Cmp(childExpected) >= 0) // because of London fork
		} else {
			// this check is implemented because non-validators incur fees, potentially resulting in a balance lower than anticipated
			require.True(t, balance.Cmp(expectedValue.Sub(childExpected, offset)) >= 0)
		}
	}

	t.Run("check the balances at the beginning", func(t *testing.T) {
		// check the balances on root and child at the beginning to see if they are as expected
		checkBalancesFn(nonValidatorKey.Address(), bigZero, command.DefaultPremineBalance, false)
		checkBalancesFn(rewardWalletKey.Address(), bigZero, command.DefaultPremineBalance, true)

		validatorsExpectedBalance := new(big.Int).Sub(command.DefaultPremineBalance, command.DefaultStake)

		for _, server := range cluster.Servers {
			validatorAccount, err := validatorHelper.GetAccountFromDir(server.DataDir())
			require.NoError(t, err)

			checkBalancesFn(validatorAccount.Address(), bigZero, validatorsExpectedBalance, true)
		}
	})

	// this test case will check first if they can withdraw some of the premined amount of non-mintable token
	t.Run("do a withdraw for premined validator address and premined non-validator address", func(t *testing.T) {
		waitOneBlockFn := func(server *framework.TestServer) {
			currentBlock, err := server.JSONRPC().GetBlockByNumber(jsonrpc.LatestBlockNumber, false)
			require.NoError(t, err)
			require.NoError(t, cluster.WaitForBlock(currentBlock.Header.Number+1, time.Second*5))
		}

		validatorSrv := cluster.Servers[1]
		validatorAcc, err := validatorHelper.GetAccountFromDir(validatorSrv.DataDir())
		require.NoError(t, err)

		validatorRawKey, err := validatorAcc.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		err = cluster.Bridges[bridgeOne].Withdraw(
			common.ERC20,
			hex.EncodeToString(validatorRawKey),
			validatorAcc.Address().String(),
			tokensToTransfer.String(),
			"",
			validatorSrv.JSONRPCAddr(),
			bridgeCfg.InternalERC20PredicateAddr,
			contracts.NativeERC20TokenContract,
			false)
		require.NoError(t, err)

		// Wait for 1 block before getting expected child balance
		waitOneBlockFn(validatorSrv)

		validatorBalanceAfterWithdraw, err := childEthEndpoint.GetBalance(
			validatorAcc.Address(), jsonrpc.LatestBlockNumberOrHash)
		require.NoError(t, err)

		err = cluster.Bridges[bridgeOne].Withdraw(
			common.ERC20,
			hex.EncodeToString(nonValidatorKeyRaw),
			nonValidatorKey.Address().String(),
			tokensToTransfer.String(),
			"",
			validatorSrv.JSONRPCAddr(),
			bridgeCfg.InternalERC20PredicateAddr,
			contracts.NativeERC20TokenContract,
			false)
		require.NoError(t, err)

		// Wait for 1 block before getting expected child balance
		waitOneBlockFn(validatorSrv)

		nonValidatorBalanceAfterWithdraw, err := childEthEndpoint.GetBalance(
			nonValidatorKey.Address(), jsonrpc.LatestBlockNumberOrHash)
		require.NoError(t, err)

		currentBlock, err := childEthEndpoint.GetBlockByNumber(jsonrpc.LatestBlockNumber, false)
		require.NoError(t, err)

		currentExtra, err := polytypes.GetIbftExtra(currentBlock.Header.ExtraData)
		require.NoError(t, err)

		t.Logf("Latest block number: %d, epoch number: %d\n", currentBlock.Number(), currentExtra.BlockMetaData.EpochNumber)

		require.NoError(t, cluster.WaitUntil(time.Minute*3, time.Second*2, func() bool {
			for bridgeEventID := uint64(1); bridgeEventID <= bridgeEvents; bridgeEventID++ {
				if !isEventProcessed(t, bridgeCfg.ExternalGatewayAddr, externalChainTxRelayer, bridgeEventID) {
					return false
				}
			}

			return true
		}))

		// assert that receiver's balances on RootERC20 smart contract are expected
		checkBalancesFn(validatorAcc.Address(), tokensToTransfer, validatorBalanceAfterWithdraw, true)
		checkBalancesFn(nonValidatorKey.Address(), tokensToTransfer, nonValidatorBalanceAfterWithdraw, false)
	})

	t.Run("do a deposit to some validator and non-validator address", func(t *testing.T) {
		validatorSrv := cluster.Servers[4]
		validatorAcc, err := validatorHelper.GetAccountFromDir(validatorSrv.DataDir())
		require.NoError(t, err)

		require.NoError(t, cluster.Bridges[bridgeOne].Deposit(
			common.ERC20,
			bridgeCfg.ExternalNativeERC20Addr,
			bridgeCfg.ExternalERC20PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join([]string{validatorAcc.Address().String(), nonValidatorKey.Address().String()}, ","),
			strings.Join([]string{tokensToTransfer.String(), tokensToTransfer.String()}, ","),
			"",
			cluster.Bridges[bridgeOne].JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
		)

		currentBlock, err := childEthEndpoint.GetBlockByNumber(jsonrpc.LatestBlockNumber, false)
		require.NoError(t, err)

		// wait for couple of epoches
		finalBlockNum := currentBlock.Header.Number + 2*epochSize
		require.NoError(t, cluster.WaitForBlock(finalBlockNum, 2*time.Minute))

		// the transaction is processed and there should be a success event
		var bridgeMessageResult contractsapi.BridgeMessageResultEvent

		for i := uint64(0); i < numberOfAttempts; i++ {
			logs, err := getFilteredLogs(bridgeMessageResult.Sig(), 0, finalBlockNum+i*epochSize, childEthEndpoint)
			require.NoError(t, err)

			if len(logs) == stateSyncedLogsCount || i == numberOfAttempts-1 {
				// assert that all deposits are executed successfully
				assertBridgeEventResultSuccess(t, logs, stateSyncedLogsCount)

				break
			}

			require.NoError(t, cluster.WaitForBlock(finalBlockNum+(i+1)*epochSize, time.Minute))
		}
	})

	t.Run("transfer more native tokens than 0x0 balance is", func(t *testing.T) {
		const expectedStateSyncsCount = 1

		// since bridging native token is essentially minting
		// (i.e. transferring tokens from 0x0 to receiver address using native transfer precompile),
		// this test tries to deposit more tokens than 0x0 address has on its balance
		currentBlock, err := childEthEndpoint.GetBlockByNumber(jsonrpc.LatestBlockNumber, false)
		require.NoError(t, err)

		require.NoError(t, cluster.Bridges[bridgeOne].Deposit(
			common.ERC20,
			bridgeCfg.ExternalNativeERC20Addr,
			bridgeCfg.ExternalERC20PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join([]string{nonValidatorKey.Address().String()}, ","),
			strings.Join([]string{tenMilionTokens.String()}, ","),
			"",
			cluster.Bridges[bridgeOne].JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
		)

		// wait for couple of epoches
		finalBlockNum := currentBlock.Header.Number + epochSize
		require.NoError(t, cluster.WaitForBlock(finalBlockNum, 2*time.Minute))

		// the transaction is processed and there should be a success event
		var bridgeMessageResult contractsapi.BridgeMessageResultEvent

		for i := uint64(0); i < numberOfAttempts; i++ {
			logs, err := getFilteredLogs(bridgeMessageResult.Sig(), currentBlock.Number()+1, finalBlockNum+i*epochSize, childEthEndpoint)
			require.NoError(t, err)

			if len(logs) == expectedStateSyncsCount || i == numberOfAttempts-1 {
				// assert that sent deposit has failed
				checkBridgeMessageResultLogs(t, logs, expectedStateSyncsCount,
					func(t *testing.T, ssre contractsapi.BridgeMessageResultEvent) {
						t.Helper()

						require.False(t, ssre.Status)
					})

				break
			}

			require.NoError(t, cluster.WaitForBlock(finalBlockNum+(i+1)*epochSize, time.Minute))
		}
	})
}

func TestE2E_Bridge_L1OriginatedNativeToken_ERC20StakingToken(t *testing.T) {
	const (
		epochSize       = 5
		blockTimeout    = 30 * time.Second
		numberOfBridges = 1
	)

	var (
		initialStake   = ethgo.Ether(10)
		addedStake     = ethgo.Ether(1)
		stakeTokenAddr = types.StringToAddress("0x2040")
	)

	minter, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	cluster := framework.NewTestCluster(t, 5,
		framework.WithNumBlockConfirmations(0),
		framework.WithEpochSize(epochSize),
		framework.WithBridges(numberOfBridges),
		framework.WithBladeAdmin(minter.Address().String()),
		framework.WithSecretsCallback(func(addrs []types.Address, tcc *framework.TestClusterConfig) {
			for i := 0; i < len(addrs); i++ {
				tcc.StakeAmounts = append(tcc.StakeAmounts, initialStake)
			}
		}),
		framework.WithNativeTokenConfig(nativeTokenNonMintableConfig),
		framework.WithPredeploy(fmt.Sprintf("%s:RootERC20", stakeTokenAddr)),
	)
	defer cluster.Stop()

	cluster.WaitForReady(t)

	polybftCfg, err := polycfg.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	// first validator server(minter)
	firstValidator := cluster.Servers[0]
	// second validator server
	secondValidator := cluster.Servers[1]

	// validator account from second validator
	validatorAccTwo, err := validatorHelper.GetAccountFromDir(secondValidator.DataDir())
	require.NoError(t, err)

	relayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(firstValidator.JSONRPCAddr()))
	require.NoError(t, err)

	mintFn := &contractsapi.MintRootERC20Fn{
		To:     validatorAccTwo.Address(),
		Amount: addedStake,
	}

	mintInput, err := mintFn.EncodeAbi()
	require.NoError(t, err)

	nonNativeErc20 := polybftCfg.StakeTokenAddr

	mintTx := types.NewTx(types.NewDynamicFeeTx(
		types.WithTo(&nonNativeErc20),
		types.WithInput(mintInput),
	))

	receipt, err := relayer.SendTransaction(mintTx, minter)
	require.NoError(t, err)
	require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

	secondValidatorInfo, err := validatorHelper.GetValidatorInfo(validatorAccTwo.Ecdsa.Address(), relayer)
	require.NoError(t, err)
	require.True(t, secondValidatorInfo.Stake.Cmp(initialStake) == 0)

	require.NoError(t, cluster.WaitForBlock(epochSize*1, blockTimeout))

	require.NoError(t, secondValidator.Stake(polybftCfg.StakeTokenAddr, addedStake))

	require.NoError(t, cluster.WaitForBlock(epochSize*3, blockTimeout))

	secondValidatorInfo, err = validatorHelper.GetValidatorInfo(validatorAccTwo.Ecdsa.Address(), relayer)
	require.NoError(t, err)

	expectedStakeAmount := new(big.Int).Add(initialStake, addedStake)
	require.Equal(t, expectedStakeAmount, secondValidatorInfo.Stake)

	require.NoError(t, secondValidator.Unstake(addedStake))

	require.NoError(t, cluster.WaitForBlock(epochSize*4, blockTimeout))

	secondValidatorInfo, err = validatorHelper.GetValidatorInfo(validatorAccTwo.Ecdsa.Address(), relayer)
	require.NoError(t, err)
	require.True(t, secondValidatorInfo.Stake.Cmp(initialStake) == 0)
}
