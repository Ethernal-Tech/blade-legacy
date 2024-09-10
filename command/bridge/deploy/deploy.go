package deploy

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/bridge/helper"
	cmdHelper "github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	// params are the parameters of CLI command
	params deployParams

	// consensusCfg contains consensus protocol configuration parameters
	consensusCfg polybft.PolyBFTConfig
)

type deploymentResultInfo struct {
	BridgeCfg      *polybft.BridgeConfig
	CommandResults []command.CommandResult
}

// GetCommand returns the bridge deploy command
func GetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deploy",
		Short:   "Deploys and initializes required smart contracts on the rootchain",
		PreRunE: preRunCommand,
		Run:     runCommand,
	}

	cmd.Flags().StringVar(
		&params.genesisPath,
		helper.GenesisPathFlag,
		helper.DefaultGenesisPath,
		helper.GenesisPathFlagDesc,
	)

	cmd.Flags().StringVar(
		&params.deployerKey,
		deployerKeyFlag,
		"",
		"hex-encoded private key of the account which deploys rootchain contracts",
	)

	cmd.Flags().StringVar(
		&params.externalRPCAddress,
		externalRPCFlag,
		txrelayer.DefaultRPCAddress,
		"the JSON RPC external chain IP address",
	)

	cmd.Flags().StringVar(
		&params.externalRPCAddress,
		internalRPCFlag,
		txrelayer.DefaultRPCAddress,
		"the JSON RPC blade chain IP address",
	)

	cmd.Flags().StringVar(
		&params.rootERC20TokenAddr,
		erc20AddrFlag,
		"",
		"existing root native erc20 token address, that originates from a rootchain",
	)

	cmd.Flags().BoolVar(
		&params.isTestMode,
		helper.TestModeFlag,
		false,
		"test indicates whether rootchain contracts deployer is hardcoded test account"+
			" (otherwise provided secrets are used to resolve deployer account)",
	)

	cmd.Flags().StringVar(
		&params.proxyContractsAdmin,
		helper.ProxyContractsAdminFlag,
		"",
		helper.ProxyContractsAdminDesc,
	)

	cmd.Flags().DurationVar(
		&params.txTimeout,
		cmdHelper.TxTimeoutFlag,
		txrelayer.DefaultTimeoutTransactions,
		cmdHelper.TxTimeoutDesc,
	)

	cmd.Flags().BoolVar(
		&params.isBootstrap,
		isBootstrapFlag,
		true,
		"indicates if bridge depoly command is run in bootstraping of blade chain. "+
			"If it is run on a live blade chain, the command will deploy and initialize internal predicates, "+
			"otherwise it will pre-allocate the internal predicates addresses in genesis",
	)

	cmd.MarkFlagsMutuallyExclusive(helper.TestModeFlag, deployerKeyFlag)

	return cmd
}

func preRunCommand(_ *cobra.Command, _ []string) error {
	return params.validateFlags()
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)
	defer outputter.WriteOutput()

	outputter.WriteCommandResult(&helper.MessageResult{
		Message: fmt.Sprintf("%s started... External Chain JSON RPC address %s.",
			contractsDeploymentTitle, params.externalRPCAddress),
	})

	chainConfig, err := chain.ImportFromFile(params.genesisPath)
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to read chain configuration: %w", err))

		return
	}

	externalChainClient, err := jsonrpc.NewEthClient(params.externalRPCAddress)
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to initialize JSON RPC client for provided IP address: %s: %w",
			params.externalRPCAddress, err))

		return
	}

	externalChainID, err := externalChainClient.ChainID()
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to get chainID for provided IP address: %s: %w",
			params.externalRPCAddress, err))
	}

	if consensusCfg.Bridge[externalChainID.Uint64()] != nil {
		code, err := externalChainClient.GetCode(consensusCfg.Bridge[externalChainID.Uint64()].ExternalGatewayAddr,
			jsonrpc.LatestBlockNumberOrHash)
		if err != nil {
			outputter.SetError(fmt.Errorf("failed to check if rootchain contracts are deployed: %w", err))

			return
		} else if code != "0x" {
			outputter.SetCommandResult(&helper.MessageResult{
				Message: fmt.Sprintf("%s contracts are already deployed. Aborting.", contractsDeploymentTitle),
			})

			return
		}
	}

	// set event tracker start blocks for rootchain contract(s) of interest
	// the block number should be queried before deploying contracts so that no events during deployment
	// and initialization are missed
	blockNum, err := externalChainClient.BlockNumber()
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to query rootchain latest block number: %w", err))

		return
	}

	deploymentResultInfo, err := deployContracts(outputter, externalChainClient, externalChainID,
		chainConfig, consensusCfg.InitialValidatorSet, cmd.Context())
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to deploy rootchain contracts: %w", err))
		outputter.SetCommandResult(command.Results(deploymentResultInfo.CommandResults))

		return
	}

	// populate bridge configuration
	consensusCfg.Bridge[externalChainID.Uint64()] = deploymentResultInfo.BridgeCfg

	consensusCfg.Bridge[externalChainID.Uint64()].EventTrackerStartBlocks = map[types.Address]uint64{
		deploymentResultInfo.BridgeCfg.ExternalGatewayAddr: blockNum,
	}

	// write updated consensus configuration
	chainConfig.Params.Engine[polybft.ConsensusName] = consensusCfg

	if err := cmdHelper.WriteGenesisConfigToDisk(chainConfig, params.genesisPath); err != nil {
		outputter.SetError(fmt.Errorf("failed to save chain configuration bridge data: %w", err))

		return
	}

	deploymentResultInfo.CommandResults = append(deploymentResultInfo.CommandResults, &helper.MessageResult{
		Message: fmt.Sprintf("%s finished. All contracts are successfully deployed and initialized.",
			contractsDeploymentTitle),
	})
	outputter.SetCommandResult(command.Results(deploymentResultInfo.CommandResults))
}

// deployContracts deploys and initializes rootchain smart contracts
func deployContracts(outputter command.OutputFormatter,
	externalChainClient *jsonrpc.EthClient,
	externalChainID *big.Int,
	chainCfg *chain.Chain,
	initialValidators []*validator.GenesisValidator,
	cmdCtx context.Context) (deploymentResultInfo, error) {
	var internalTxRelayer txrelayer.TxRelayer

	externalTxRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithClient(externalChainClient),
		txrelayer.WithWriter(outputter),
		txrelayer.WithReceiptsTimeout(params.txTimeout))
	if err != nil {
		return deploymentResultInfo{BridgeCfg: nil, CommandResults: nil},
			fmt.Errorf("failed to initialize tx relayer for external chain: %w", err)
	}

	deployerKey, err := helper.DecodePrivateKey(params.deployerKey)
	if err != nil {
		return deploymentResultInfo{BridgeCfg: nil, CommandResults: nil},
			fmt.Errorf("failed to initialize deployer key: %w", err)
	}

	if params.isTestMode {
		deployerAddr := deployerKey.Address()

		txn := helper.CreateTransaction(types.ZeroAddress, &deployerAddr, nil, ethgo.Ether(1), true)
		if _, err = externalTxRelayer.SendTransactionLocal(txn); err != nil {
			return deploymentResultInfo{BridgeCfg: nil, CommandResults: nil}, err
		}
	}

	internalChainID := chainCfg.Params.ChainID
	bridgeConfig := &polybft.BridgeConfig{JSONRPCEndpoint: params.externalRPCAddress}

	// setup external contracts
	if err := initExternalContracts(outputter, chainCfg, bridgeConfig,
		externalChainClient, externalChainID); err != nil {
		return deploymentResultInfo{BridgeCfg: nil, CommandResults: nil}, err
	}

	// setup internal contracts
	initInternalContracts(outputter, chainCfg)

	// pre-allocate internal predicates addresses in genesis if blade is bootstrapping
	if params.isBootstrap {
		if err := preAllocateInternalPredicates(outputter, chainCfg, bridgeConfig); err != nil {
			return deploymentResultInfo{BridgeCfg: nil, CommandResults: nil}, err
		}

		internalTxRelayer, err = txrelayer.NewTxRelayer(txrelayer.WithIPAddress(params.internalRPCAddress),
			txrelayer.WithWriter(outputter), txrelayer.WithReceiptsTimeout(params.txTimeout))
		if err != nil {
			return deploymentResultInfo{BridgeCfg: nil, CommandResults: nil},
				fmt.Errorf("failed to initialize tx relayer for internal chain: %w", err)
		}
	}

	g, ctx := errgroup.WithContext(cmdCtx)
	results := make(map[string]*deployContractResult, 0)
	resultsLock := sync.Mutex{}
	proxyAdmin := types.StringToAddress(params.proxyContractsAdmin)

	deployContractFn := func(contract *contract, txr txrelayer.TxRelayer) {
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				deployResults, err := contract.deploy(bridgeConfig, txr, deployerKey, proxyAdmin)
				if err != nil {
					return err
				}

				resultsLock.Lock()
				defer resultsLock.Unlock()

				for _, deployResult := range deployResults {
					results[deployResult.Name] = deployResult
				}

				return nil
			}
		})
	}

	initializeContractFn := func(contract *contract, txr txrelayer.TxRelayer, chainID int64) {
		if contract.initializeFn == nil {
			// some contracts do not have initialize function
			return
		}

		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return contract.initializeFn(outputter, externalTxRelayer,
					initialValidators, bridgeConfig, deployerKey, chainID)

			}
		})
	}

	// deploy external contracts
	for _, contract := range externalContracts {
		deployContractFn(contract, externalTxRelayer)
	}

	// deploy internal contracts if blade is live
	if !params.isBootstrap {
		for _, contract := range internalContracts {
			deployContractFn(contract, internalTxRelayer)
		}
	}

	// wait for all contracts to be deployed
	if err := g.Wait(); err != nil {
		return collectResultsOnError(results), err
	}

	// initialize contracts only after all of them are deployed
	g, ctx = errgroup.WithContext(cmdCtx)

	// initialize external contracts
	for _, contract := range externalContracts {
		initializeContractFn(contract, externalTxRelayer, internalChainID)
	}

	// initialize internal contracts if blade is live
	if !params.isBootstrap {
		for _, contract := range internalContracts {
			initializeContractFn(contract, internalTxRelayer, externalChainID.Int64())
		}
	}

	// wait for all contracts to be initialized
	if err := g.Wait(); err != nil {
		return deploymentResultInfo{BridgeCfg: nil, CommandResults: nil}, err
	}

	commandResults := make([]command.CommandResult, 0, len(results))
	for _, result := range results {
		commandResults = append(commandResults, result)
	}

	return deploymentResultInfo{
		BridgeCfg:      bridgeConfig,
		CommandResults: commandResults}, nil
}

// initContract initializes arbitrary contract with given parameters deployed on a given address
func initContract(cmdOutput command.OutputFormatter, txRelayer txrelayer.TxRelayer,
	initInputFn contractsapi.StateTransactionInput, contractAddr types.Address,
	contractName string, deployerKey crypto.Key) error {
	input, err := initInputFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("failed to encode initialization params for %s.initialize. error: %w",
			contractName, err)
	}

	if _, err := helper.SendTransaction(txRelayer, contractAddr,
		input, contractName, deployerKey); err != nil {
		return err
	}

	cmdOutput.WriteCommandResult(
		&helper.MessageResult{
			Message: fmt.Sprintf("%s %s contract is initialized", contractsDeploymentTitle, contractName),
		})

	return nil
}

func collectResultsOnError(results map[string]*deployContractResult) deploymentResultInfo {
	commandResults := make([]command.CommandResult, 0, len(results)+1)
	messageResult := helper.MessageResult{Message: "[BRIDGE - DEPLOY] Successfully deployed the following contracts\n"}

	for _, result := range results {
		if result != nil {
			// In case an error happened, some of the indices may not be populated.
			// Filter those out.
			commandResults = append(commandResults, result)
		}
	}

	commandResults = append([]command.CommandResult{messageResult}, commandResults...)

	return deploymentResultInfo{
		BridgeCfg: nil,

		CommandResults: commandResults}
}

// getValidatorSet converts given validators to generic map
// which is used for ABI encoding validator set being sent to the Gateway contract
func getValidatorSet(o command.OutputFormatter,
	validators []*validator.GenesisValidator) ([]*contractsapi.Validator, error) {
	accSet := make(validator.AccountSet, len(validators))

	if _, err := o.Write([]byte("[VALIDATORS - GATEWAY] \n")); err != nil {
		return nil, err
	}

	for i, val := range validators {
		if _, err := o.Write([]byte(fmt.Sprintf("%v\n", val))); err != nil {
			return nil, err
		}

		blsKey, err := val.UnmarshalBLSPublicKey()
		if err != nil {
			return nil, err
		}

		accSet[i] = &validator.ValidatorMetadata{
			Address:     val.Address,
			BlsKey:      blsKey,
			VotingPower: new(big.Int).Set(val.Stake),
		}
	}

	hash, err := accSet.Hash()
	if err != nil {
		return nil, err
	}

	if _, err := o.Write([]byte(
		fmt.Sprintf("[VALIDATORS - GATEWAY] Validators hash: %s\n", hash))); err != nil {
		return nil, err
	}

	return accSet.ToAPIBinding(), nil
}
