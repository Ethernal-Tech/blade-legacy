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
		Short:   "Deploys and initializes set of bridge smart contracts",
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
		"hex-encoded private key of the account which deploys bridge contracts",
	)

	cmd.Flags().StringVar(
		&params.externalRPCAddress,
		externalRPCFlag,
		txrelayer.DefaultRPCAddress,
		"the JSON RPC external chain IP address",
	)

	cmd.Flags().StringVar(
		&params.internalRPCAddress,
		internalRPCFlag,
		txrelayer.DefaultRPCAddress,
		"the JSON RPC blade chain IP address",
	)

	cmd.Flags().StringVar(
		&params.rootERC20TokenAddr,
		erc20AddrFlag,
		"",
		"existing erc20 token address, that originates from a external chain and that gets mapped to the Blade native one",
	)

	cmd.Flags().BoolVar(
		&params.isTestMode,
		helper.TestModeFlag,
		false,
		"test indicates whether bridge contracts deployer is hardcoded test account"+
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
		"indicates if bridge deploy command is run during bootstraping of the Blade chain. "+
			"If it is run on a live Blade chain, the command will deploy and initialize internal predicates, "+
			"otherwise it will pre-allocate the internal predicates addresses in the genesis configuration.",
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
		Message: fmt.Sprintf("%s started... External chain JSON RPC address %s.",
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

	externalChainIDBig, err := externalChainClient.ChainID()
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to get chainID for provided IP address: %s: %w",
			params.externalRPCAddress, err))
	}

	externalChainID := externalChainIDBig.Uint64()
	bridgeCfg := consensusCfg.Bridge[externalChainID]

	if bridgeCfg != nil {
		code, err := externalChainClient.GetCode(bridgeCfg.ExternalGatewayAddr, jsonrpc.LatestBlockNumberOrHash)
		if err != nil {
			outputter.SetError(fmt.Errorf("failed to check if external chain bridge contracts are deployed: %w", err))

			return
		} else if code != "0x" {
			outputter.SetCommandResult(&helper.MessageResult{
				Message: fmt.Sprintf("%s contracts are already deployed. Aborting.", contractsDeploymentTitle),
			})

			return
		}
	}

	deploymentResultInfo, err := deployContracts(outputter, externalChainClient, externalChainIDBig,
		chainConfig, consensusCfg.InitialValidatorSet, cmd.Context())
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to deploy bridge contracts: %w", err))
		outputter.SetCommandResult(command.Results(deploymentResultInfo.CommandResults))

		return
	}

	// set event tracker start blocks for external chain contract(s) of interest
	// the block number should be queried before deploying contracts so that no events during deployment
	// and initialization are missed
	latestBlockNum, err := externalChainClient.BlockNumber()
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to query the external chain's latest block number: %w", err))

		return
	}

	// populate bridge configuration
	consensusCfg.Bridge[externalChainID] = deploymentResultInfo.BridgeCfg
	consensusCfg.Bridge[externalChainID].EventTrackerStartBlocks = map[types.Address]uint64{
		deploymentResultInfo.BridgeCfg.ExternalGatewayAddr: latestBlockNum,
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

// deployContracts deploys and initializes bridge smart contracts
func deployContracts(
	outputter command.OutputFormatter,
	externalChainClient *jsonrpc.EthClient,
	externalChainID *big.Int,
	chainCfg *chain.Chain,
	initialValidators []*validator.GenesisValidator,
	cmdCtx context.Context) (*deploymentResultInfo, error) {
	externalTxRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithClient(externalChainClient),
		txrelayer.WithWriter(outputter),
		txrelayer.WithReceiptsTimeout(params.txTimeout))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tx relayer for external chain: %w", err)
	}

	deployerKey, err := helper.DecodePrivateKey(params.deployerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize deployer key: %w", err)
	}

	if params.isTestMode {
		deployerAddr := deployerKey.Address()

		txn := helper.CreateTransaction(types.ZeroAddress, &deployerAddr, nil, ethgo.Ether(1), true)
		if _, err = externalTxRelayer.SendTransactionLocal(txn); err != nil {
			return nil, err
		}
	}

	var (
		internalChainID   = chainCfg.Params.ChainID
		bridgeConfig      = &polybft.BridgeConfig{JSONRPCEndpoint: params.externalRPCAddress}
		externalContracts []*contract
		internalContracts []*contract
	)

	// setup external contracts
	if externalContracts, err = initExternalContracts(bridgeConfig, externalChainClient, externalChainID); err != nil {
		return nil, err
	}

	// setup internal contracts
	internalContracts = initInternalContracts(chainCfg)

	// pre-allocate internal predicates addresses in genesis if blade is bootstrapping
	if params.isBootstrap {
		if err := preAllocateInternalPredicates(outputter, internalContracts, chainCfg, bridgeConfig); err != nil {
			return nil, err
		}
	}

	g, ctx := errgroup.WithContext(cmdCtx)
	results := make(map[string]*deployContractResult)
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
				return contract.initializeFn(outputter, txr,
					initialValidators, bridgeConfig, deployerKey, chainID)

			}
		})
	}

	// deploy external contracts
	for _, contract := range externalContracts {
		deployContractFn(contract, externalTxRelayer)
	}

	var internalTxRelayer txrelayer.TxRelayer
	// deploy internal contracts if blade is already running
	if !params.isBootstrap {
		internalTxRelayer, err = txrelayer.NewTxRelayer(txrelayer.WithIPAddress(params.internalRPCAddress),
			txrelayer.WithWriter(outputter), txrelayer.WithReceiptsTimeout(params.txTimeout))
		if err != nil {
			return nil, fmt.Errorf("failed to initialize tx relayer for internal chain: %w", err)
		}

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
		return nil, err
	}

	commandResults := make([]command.CommandResult, 0, len(results))
	for _, result := range results {
		commandResults = append(commandResults, result)
	}

	return &deploymentResultInfo{BridgeCfg: bridgeConfig, CommandResults: commandResults}, nil
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

func collectResultsOnError(results map[string]*deployContractResult) *deploymentResultInfo {
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

	return &deploymentResultInfo{BridgeCfg: nil, CommandResults: commandResults}
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

	return accSet.ToABIBinding(), nil
}
