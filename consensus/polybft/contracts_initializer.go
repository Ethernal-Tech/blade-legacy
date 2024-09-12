package polybft

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo/abi"
)

const (
	contractCallGasLimit = 100_000_000
)

func initStakeManager(polyBFTConfig PolyBFTConfig, transition *state.Transition) error {
	startValidators := make([]*contractsapi.GenesisValidator, len(polyBFTConfig.InitialValidatorSet))

	for i, validator := range polyBFTConfig.InitialValidatorSet {
		blsRaw, err := hex.DecodeHex(validator.BlsKey)
		if err != nil {
			return err
		}

		key, err := bls.UnmarshalPublicKey(blsRaw)
		if err != nil {
			return err
		}

		startValidators[i] = &contractsapi.GenesisValidator{
			Addr:   validator.Address,
			Stake:  validator.Stake,
			BlsKey: key.ToBigInt(),
		}

		approveFn := &contractsapi.ApproveNativeERC20MintableFn{
			Spender: contracts.StakeManagerContract,
			Amount:  validator.Stake,
		}

		input, err := approveFn.EncodeAbi()
		if err != nil {
			return fmt.Errorf("StakingERC20.approve params encoding failed: %w", err)
		}

		err = callContract(validator.Address, polyBFTConfig.StakeTokenAddr, input, "StakingERC20.approve", transition)
		if err != nil {
			return fmt.Errorf("error while calling contract %w", err)
		}
	}

	initFn := &contractsapi.InitializeStakeManagerFn{
		GenesisValidators: startValidators,
		NewStakingToken:   polyBFTConfig.StakeTokenAddr,
		NewBls:            contracts.BLSContract,
		EpochManager:      contracts.EpochManagerContract,
		NewDomain:         signer.DomainValidatorSetString,
		Owner:             polyBFTConfig.BladeAdmin,
		NetworkParams:     polyBFTConfig.GovernanceConfig.NetworkParamsAddr,
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("StakeManager.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.StakeManagerContract, input, "StakeManager.initialize", transition)
}

// initEpochManager initializes EpochManager SC
func initEpochManager(polyBFTConfig PolyBFTConfig, transition *state.Transition) error {
	initFn := &contractsapi.InitializeEpochManagerFn{
		NewRewardToken:   polyBFTConfig.RewardConfig.TokenAddress,
		NewRewardWallet:  polyBFTConfig.RewardConfig.WalletAddress,
		NewStakeManager:  contracts.StakeManagerContract,
		NewNetworkParams: polyBFTConfig.GovernanceConfig.NetworkParamsAddr,
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("EpochManager.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.EpochManagerContract, input, "EpochManager.initialize", transition)
}

// getInitERC20PredicateInput builds initialization input parameters for child chain ERC20Predicate SC
func getInitERC20PredicateInput(config *BridgeConfig, childChainMintable bool, chainID *big.Int) ([]byte, error) {
	var params contractsapi.StateTransactionInput
	if childChainMintable {
		params = &contractsapi.InitializeRootERC20PredicateFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewChildERC20Predicate:      config.ExternalMintableERC20PredicateAddr,
			NewDestinationTokenTemplate: config.ExternalERC20Addr,
			NewDestinationChainID:       chainID,
		}
	} else {
		params = &contractsapi.InitializeChildERC20PredicateFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewRootERC20Predicate:       config.ExternalERC20PredicateAddr,
			NewDestinationTokenTemplate: contracts.ChildERC20Contract,
			NewNativeTokenRootAddress:   config.ExternalNativeERC20Addr,
			NewDestinationChainID:       chainID,
		}
	}

	return params.EncodeAbi()
}

// getInitERC20PredicateACLInput builds initialization input parameters for child chain ERC20PredicateAccessList SC
func getInitERC20PredicateACLInput(config *BridgeConfig, owner types.Address,
	useAllowList, useBlockList, childChainMintable bool, chainID *big.Int) ([]byte, error) {
	var params contractsapi.StateTransactionInput
	if childChainMintable {
		params = &contractsapi.InitializeRootMintableERC20PredicateACLFn{
			NewGateway:             config.InternalGatewayAddr,
			NewChildERC20Predicate: config.ExternalMintableERC20PredicateAddr,
			NewTokenTemplate:       config.ExternalERC20Addr,
			NewDestinationChainID:  chainID,
			NewUseAllowList:        useAllowList,
			NewUseBlockList:        useBlockList,
			NewOwner:               owner,
		}
	} else {
		params = &contractsapi.InitializeChildERC20PredicateACLFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewRootERC20Predicate:       config.ExternalERC20PredicateAddr,
			NewDestinationTokenTemplate: contracts.ChildERC20Contract,
			NewDestinationChainID:       chainID,
			NewNativeTokenRootAddress:   config.ExternalNativeERC20Addr,
			NewUseAllowList:             useAllowList,
			NewUseBlockList:             useBlockList,
			NewOwner:                    owner,
		}
	}

	return params.EncodeAbi()
}

// getInitERC721PredicateInput builds initialization input parameters for child chain ERC721Predicate SC
func getInitERC721PredicateInput(config *BridgeConfig, childOriginatedTokens bool, chainID *big.Int) ([]byte, error) {
	var params contractsapi.StateTransactionInput
	if childOriginatedTokens {
		params = &contractsapi.InitializeRootERC721PredicateFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewChildERC721Predicate:     config.ExternalMintableERC721PredicateAddr,
			NewDestinationTokenTemplate: config.ExternalERC721Addr,
			NewDestinationChainID:       chainID,
		}
	} else {
		params = &contractsapi.InitializeChildERC721PredicateFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewRootERC721Predicate:      config.ExternalERC721PredicateAddr,
			NewDestinationTokenTemplate: contracts.ChildERC721Contract,
			NewDestinationChainID:       chainID,
		}
	}

	return params.EncodeAbi()
}

// getInitERC721PredicateACLInput builds initialization input parameters
// for child chain ERC721PredicateAccessList SC
func getInitERC721PredicateACLInput(config *BridgeConfig, owner types.Address,
	useAllowList, useBlockList, childChainMintable bool, chainID *big.Int) ([]byte, error) {
	var params contractsapi.StateTransactionInput
	if childChainMintable {
		params = &contractsapi.InitializeRootMintableERC721PredicateACLFn{
			NewGateway:              config.InternalGatewayAddr,
			NewChildERC721Predicate: config.ExternalMintableERC721PredicateAddr,
			NewTokenTemplate:        config.ExternalERC721Addr,
			NewDestinationChainID:   chainID,
			NewUseAllowList:         useAllowList,
			NewUseBlockList:         useBlockList,
			NewOwner:                owner,
		}
	} else {
		params = &contractsapi.InitializeChildERC721PredicateACLFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewRootERC721Predicate:      config.ExternalERC721PredicateAddr,
			NewDestinationTokenTemplate: contracts.ChildERC721Contract,
			NewDestinationChainID:       chainID,
			NewUseAllowList:             useAllowList,
			NewUseBlockList:             useBlockList,
			NewOwner:                    owner,
		}
	}

	return params.EncodeAbi()
}

// getInitERC1155PredicateInput builds initialization input parameters for child chain ERC1155Predicate SC
func getInitERC1155PredicateInput(config *BridgeConfig, childChainMintable bool, chainID *big.Int) ([]byte, error) {
	var params contractsapi.StateTransactionInput
	if childChainMintable {
		params = &contractsapi.InitializeRootERC1155PredicateFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewChildERC1155Predicate:    config.ExternalMintableERC1155PredicateAddr,
			NewDestinationTokenTemplate: config.ExternalERC1155Addr,
			NewDestinationChainID:       chainID,
		}
	} else {
		params = &contractsapi.InitializeChildERC1155PredicateFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewRootERC1155Predicate:     config.ExternalERC1155PredicateAddr,
			NewDestinationTokenTemplate: contracts.ChildERC1155Contract,
			NewDestinationChainID:       chainID,
		}
	}

	return params.EncodeAbi()
}

// getInitERC1155PredicateACLInput builds initialization input parameters
// for child chain ERC1155PredicateAccessList SC
func getInitERC1155PredicateACLInput(config *BridgeConfig, owner types.Address,
	useAllowList, useBlockList, childChainMintable bool, chainID *big.Int) ([]byte, error) {
	var params contractsapi.StateTransactionInput
	if childChainMintable {
		params = &contractsapi.InitializeRootMintableERC1155PredicateACLFn{
			NewGateway:               config.InternalGatewayAddr,
			NewChildERC1155Predicate: config.ExternalMintableERC1155PredicateAddr,
			NewTokenTemplate:         config.ExternalERC1155Addr,
			NewDestinationChainID:    chainID,
			NewUseAllowList:          useAllowList,
			NewUseBlockList:          useBlockList,
			NewOwner:                 owner,
		}
	} else {
		params = &contractsapi.InitializeChildERC1155PredicateACLFn{
			NewGateway:                  config.InternalGatewayAddr,
			NewRootERC1155Predicate:     config.ExternalERC1155PredicateAddr,
			NewDestinationTokenTemplate: contracts.ChildERC1155Contract,
			NewDestinationChainID:       chainID,
			NewUseAllowList:             useAllowList,
			NewUseBlockList:             useBlockList,
			NewOwner:                    owner,
		}
	}

	return params.EncodeAbi()
}

// initNetworkParamsContract initializes NetworkParams contract on child chain
func initNetworkParamsContract(baseFeeChangeDenom uint64, cfg PolyBFTConfig,
	transition *state.Transition) error {
	initFn := &contractsapi.InitializeNetworkParamsFn{
		InitParams: &contractsapi.InitParams{
			// only timelock controller can execute transactions on network params
			// so we set it as its owner
			NewOwner:                   cfg.GovernanceConfig.ChildTimelockAddr,
			NewCheckpointBlockInterval: new(big.Int).SetUint64(cfg.CheckpointInterval),
			NewSprintSize:              new(big.Int).SetUint64(cfg.SprintSize),
			NewEpochSize:               new(big.Int).SetUint64(cfg.EpochSize),
			NewEpochReward:             new(big.Int).SetUint64(cfg.EpochReward),
			NewMinValidatorSetSize:     new(big.Int).SetUint64(cfg.MinValidatorSetSize),
			NewMaxValidatorSetSize:     new(big.Int).SetUint64(cfg.MaxValidatorSetSize),
			NewWithdrawalWaitPeriod:    new(big.Int).SetUint64(cfg.WithdrawalWaitPeriod),
			NewBlockTime:               new(big.Int).SetUint64(uint64(cfg.BlockTime.Duration)),
			NewBlockTimeDrift:          new(big.Int).SetUint64(cfg.BlockTimeDrift),
			NewVotingDelay:             new(big.Int).Set(cfg.GovernanceConfig.VotingDelay),
			NewVotingPeriod:            new(big.Int).Set(cfg.GovernanceConfig.VotingPeriod),
			NewProposalThreshold:       new(big.Int).Set(cfg.GovernanceConfig.ProposalThreshold),
			NewBaseFeeChangeDenom:      new(big.Int).SetUint64(baseFeeChangeDenom),
		},
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("NetworkParams.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		cfg.GovernanceConfig.NetworkParamsAddr, input, "NetworkParams.initialize", transition)
}

// initForkParamsContract initializes ForkParams contract on child chain
func initForkParamsContract(cfg PolyBFTConfig, transition *state.Transition) error {
	initFn := &contractsapi.InitializeForkParamsFn{
		NewOwner: cfg.GovernanceConfig.ChildTimelockAddr,
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("ForkParams.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		cfg.GovernanceConfig.ForkParamsAddr, input, "ForkParams.initialize", transition)
}

// initChildTimelock initializes ChildTimelock contract on child chain
func initChildTimelock(cfg PolyBFTConfig, transition *state.Transition) error {
	addresses := make([]types.Address, len(cfg.InitialValidatorSet)+1)
	// we need to add child governor to list of proposers and executors as well
	addresses[0] = cfg.GovernanceConfig.ChildGovernorAddr

	for i := 0; i < len(cfg.InitialValidatorSet); i++ {
		addresses[i+1] = cfg.InitialValidatorSet[i].Address
	}

	initFn := &contractsapi.InitializeChildTimelockFn{
		Admin:     cfg.BladeAdmin,
		Proposers: addresses,
		Executors: addresses,
		MinDelay:  big.NewInt(1), // for now
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("ChildTimelock.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		cfg.GovernanceConfig.ChildTimelockAddr, input, "ChildTimelock.initialize", transition)
}

// initChildGovernor initializes ChildGovernor contract on child chain
func initChildGovernor(cfg PolyBFTConfig, transition *state.Transition) error {
	addresses := make([]types.Address, len(cfg.InitialValidatorSet))
	for i := 0; i < len(cfg.InitialValidatorSet); i++ {
		addresses[i] = cfg.InitialValidatorSet[i].Address
	}

	initFn := &contractsapi.InitializeChildGovernorFn{
		Token_:           contracts.StakeManagerContract,
		Timelock_:        cfg.GovernanceConfig.ChildTimelockAddr,
		NetworkParams:    cfg.GovernanceConfig.NetworkParamsAddr,
		QuorumNumerator_: new(big.Int).SetUint64(cfg.GovernanceConfig.ProposalQuorumPercentage),
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("ChildGovernor.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		cfg.GovernanceConfig.ChildGovernorAddr, input, "ChildGovernor.initialize", transition)
}

// initBridgeStorageContract initializes BridgeStorage contract on blade chain
func initBridgeStorageContract(cfg PolyBFTConfig, transition *state.Transition) error {
	validators, err := getValidatorStorageValidators(cfg.InitialValidatorSet)
	if err != nil {
		return fmt.Errorf("error while converting validators for bridge storage contract: %w", err)
	}

	initFn := &contractsapi.InitializeBridgeStorageFn{
		NewBls:     contracts.BLSContract,
		NewBn256G2: contracts.BLS256Contract,
		Validators: validators,
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("BridgeStorage.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller, contracts.BridgeStorageContract, input,
		"BridgeStorage.initialize", transition)
}

// initGatewayContract initializes Gateway contract on blade chain
func initGatewayContract(cfg PolyBFTConfig, bridgeCfg *BridgeConfig,
	transition *state.Transition, alloc map[types.Address]*chain.GenesisAccount) error {
	implementationAddr := bridgeCfg.InternalGatewayAddr.IncrementBy(1)
	if _, exists := alloc[implementationAddr]; !exists {
		// we do not initialize gateway contract for bridges that were added
		// after the genesis block
		return nil
	}

	validators, err := getValidatorStorageValidators(cfg.InitialValidatorSet)
	if err != nil {
		return fmt.Errorf("error while converting validators for gateway contract: %w", err)
	}

	initFn := &contractsapi.InitializeGatewayFn{
		NewBls:     contracts.BLSContract,
		NewBn256G2: contracts.BLS256Contract,
		Validators: validators,
	}

	input, err := initFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("gateway.initialize params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller, bridgeCfg.InternalGatewayAddr, input,
		"Gateway.initialize", transition)
}

// initERC20ACLPredicateContract initializes ChildERC20Predicate with access list contract on blade chain
func initERC20ACLPredicateContract(
	transition *state.Transition,
	bcfg *BridgeConfig,
	alloc map[types.Address]*chain.GenesisAccount,
	owner types.Address,
	useBridgeAllowList, useBridgeBlockList, childMintable bool,
	chainID *big.Int,
	contractName string) error {
	contractAddr := bcfg.InternalMintableERC20PredicateAddr
	if !childMintable {
		contractAddr = bcfg.InternalERC20PredicateAddr
	}

	implementationAddr := contractAddr.IncrementBy(1)
	if _, exists := alloc[implementationAddr]; !exists {
		// we do not initialize child predicates for bridges that were added
		// after the genesis block
		return nil
	}

	input, err := getInitERC20PredicateACLInput(bcfg, owner,
		useBridgeAllowList, useBridgeBlockList, childMintable, chainID)
	if err != nil {
		return err
	}

	if err = callContract(contracts.SystemCaller, contractAddr, input, contractName, transition); err != nil {
		return err
	}

	return nil
}

// initERC721ACLPredicateContract initializes ChildERC721Predicate with access list contract on blade chain
func initERC721ACLPredicateContract(
	transition *state.Transition,
	bcfg *BridgeConfig,
	alloc map[types.Address]*chain.GenesisAccount,
	owner types.Address,
	useBridgeAllowList, useBridgeBlockList, childMintable bool,
	chainID *big.Int,
	contractName string) error {
	contractAddr := bcfg.InternalMintableERC721PredicateAddr
	if !childMintable {
		contractAddr = bcfg.InternalERC721PredicateAddr
	}

	implementationAddr := contractAddr.IncrementBy(1)
	if _, exists := alloc[implementationAddr]; !exists {
		// we do not initialize child predicates for bridges that were added
		// after the genesis block
		return nil
	}

	input, err := getInitERC721PredicateACLInput(bcfg, owner,
		useBridgeAllowList, useBridgeBlockList, childMintable, chainID)
	if err != nil {
		return err
	}

	if err = callContract(contracts.SystemCaller, contractAddr, input, contractName, transition); err != nil {
		return err
	}

	return nil
}

// initERC1155ACLPredicateContract initializes ChildERC1155Predicate with access list contract on blade chain
func initERC1155ACLPredicateContract(
	transition *state.Transition,
	bcfg *BridgeConfig,
	alloc map[types.Address]*chain.GenesisAccount,
	owner types.Address,
	useBridgeAllowList, useBridgeBlockList, childMintable bool,
	chainID *big.Int,
	contractName string) error {
	contractAddr := bcfg.InternalMintableERC1155PredicateAddr
	if !childMintable {
		contractAddr = bcfg.InternalERC1155PredicateAddr
	}

	implementationAddr := contractAddr.IncrementBy(1)
	if _, exists := alloc[implementationAddr]; !exists {
		// we do not initialize child predicates for bridges that were added
		// after the genesis block
		return nil
	}

	input, err := getInitERC1155PredicateACLInput(bcfg, owner,
		useBridgeAllowList, useBridgeBlockList, childMintable, chainID)
	if err != nil {
		return err
	}

	if err = callContract(contracts.SystemCaller, contractAddr, input, contractName, transition); err != nil {
		return err
	}

	return nil
}

// initERC20PredicateContract initializes ChildERC20Predicate contract on blade chain
func initERC20PredicateContract(
	transition *state.Transition,
	bcfg *BridgeConfig,
	alloc map[types.Address]*chain.GenesisAccount,
	childMintable bool,
	chainID *big.Int,
	contractName string) error {
	contractAddr := bcfg.InternalMintableERC20PredicateAddr
	if !childMintable {
		contractAddr = bcfg.InternalERC20PredicateAddr
	}

	implementationAddr := contractAddr.IncrementBy(1)
	if _, exists := alloc[implementationAddr]; !exists {
		// we do not initialize child predicates for bridges that were added
		// after the genesis block
		return nil
	}

	input, err := getInitERC20PredicateInput(bcfg, childMintable, chainID)
	if err != nil {
		return err
	}

	if err = callContract(contracts.SystemCaller, contractAddr, input, contractName, transition); err != nil {
		return err
	}

	return nil
}

// initERC721PredicateContract initializes ChildERC721Predicate contract on blade chain
func initERC721PredicateContract(
	transition *state.Transition,
	bcfg *BridgeConfig,
	alloc map[types.Address]*chain.GenesisAccount,
	childMintable bool,
	chainID *big.Int,
	contractName string) error {
	contractAddr := bcfg.InternalMintableERC721PredicateAddr
	if !childMintable {
		contractAddr = bcfg.InternalERC721PredicateAddr
	}

	implementationAddr := contractAddr.IncrementBy(1)
	if _, exists := alloc[implementationAddr]; !exists {
		// we do not initialize child predicates for bridges that were added
		// after the genesis block
		return nil
	}

	input, err := getInitERC721PredicateInput(bcfg, childMintable, chainID)
	if err != nil {
		return err
	}

	if err = callContract(contracts.SystemCaller, contractAddr, input, contractName, transition); err != nil {
		return err
	}

	return nil
}

// initERC1155PredicateContract initializes ChildERC1155Predicate contract on blade chain
func initERC1155PredicateContract(
	transition *state.Transition,
	bcfg *BridgeConfig,
	alloc map[types.Address]*chain.GenesisAccount,
	childMintable bool,
	chainID *big.Int,
	contractName string) error {
	contractAddr := bcfg.InternalMintableERC1155PredicateAddr
	if !childMintable {
		contractAddr = bcfg.InternalERC1155PredicateAddr
	}

	implementationAddr := contractAddr.IncrementBy(1)
	if _, exists := alloc[implementationAddr]; !exists {
		// we do not initialize child predicates for bridges that were added
		// after the genesis block
		return nil
	}

	input, err := getInitERC1155PredicateInput(bcfg, childMintable, chainID)
	if err != nil {
		return err
	}

	if err = callContract(contracts.SystemCaller, contractAddr, input, contractName, transition); err != nil {
		return err
	}

	return nil
}

// mintRewardTokensToWallet mints configured amount of reward tokens to reward wallet address
func mintRewardTokensToWallet(polyBFTConfig PolyBFTConfig, transition *state.Transition) error {
	if isNativeRewardToken(polyBFTConfig) {
		// if reward token is a native erc20 token, we don't need to mint an amount of tokens
		// for given wallet address to it since this is done in premine
		return nil
	}

	mintFn := contractsapi.MintRootERC20Fn{
		To:     polyBFTConfig.RewardConfig.WalletAddress,
		Amount: polyBFTConfig.RewardConfig.WalletAmount,
	}

	input, err := mintFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("RewardERC20Token.mint params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller, polyBFTConfig.RewardConfig.TokenAddress, input,
		"RewardERC20Token.mint", transition)
}

// mintStakeToken mints configured amount of stake token to stake token address
func mintStakeToken(polyBFTConfig PolyBFTConfig, transition *state.Transition) error {
	if IsNativeStakeToken(polyBFTConfig.StakeTokenAddr) {
		return nil
	}

	for _, validator := range polyBFTConfig.InitialValidatorSet {
		mintFn := contractsapi.MintRootERC20Fn{
			To:     validator.Address,
			Amount: validator.Stake,
		}

		input, err := mintFn.EncodeAbi()
		if err != nil {
			return fmt.Errorf("StakingERC20.mint params encoding failed: %w", err)
		}

		if err := callContract(polyBFTConfig.BladeAdmin, polyBFTConfig.StakeTokenAddr,
			input, "StakingERC20.mint", transition); err != nil {
			return err
		}
	}

	return nil
}

// approveEpochManagerAsSpender approves EpochManager contract as reward token spender
// since EpochManager distributes rewards
func approveEpochManagerAsSpender(polyBFTConfig PolyBFTConfig, transition *state.Transition) error {
	approveFn := &contractsapi.ApproveRootERC20Fn{
		Spender: contracts.EpochManagerContract,
		Amount:  polyBFTConfig.RewardConfig.WalletAmount,
	}

	input, err := approveFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("RewardToken.approve params encoding failed: %w", err)
	}

	return callContract(polyBFTConfig.RewardConfig.WalletAddress,
		polyBFTConfig.RewardConfig.TokenAddress, input, "RewardToken.approve", transition)
}

// callContract calls given smart contract function, encoded in input parameter
func callContract(from, to types.Address, input []byte, contractName string, transition *state.Transition) error {
	result := transition.Call2(from, to, input, big.NewInt(0), contractCallGasLimit)
	if result.Failed() {
		if result.Reverted() {
			if revertReason, err := abi.UnpackRevertError(result.ReturnValue); err == nil {
				return fmt.Errorf("%s contract call was reverted: %s", contractName, revertReason)
			}
		}

		return fmt.Errorf("%s contract call failed: %w", contractName, result.Err)
	}

	return nil
}

// getValidatorStorageValidators converts initial validators to Validator struct
// from ValidatorStorage contract for Bridge
func getValidatorStorageValidators(initialValidators []*validator.GenesisValidator) ([]*contractsapi.Validator, error) {
	validators := make([]*contractsapi.Validator, len(initialValidators))

	for i, validator := range initialValidators {
		blsRaw, err := hex.DecodeHex(validator.BlsKey)
		if err != nil {
			return nil, err
		}

		key, err := bls.UnmarshalPublicKey(blsRaw)
		if err != nil {
			return nil, err
		}

		validators[i] = &contractsapi.Validator{
			Address:     validator.Address,
			BlsKey:      key.ToBigInt(),
			VotingPower: validator.Stake,
		}
	}

	return validators, nil
}

// isNativeRewardToken returns true in case a native token is used as a reward token as well
func isNativeRewardToken(cfg PolyBFTConfig) bool {
	return cfg.RewardConfig.TokenAddress == contracts.NativeERC20TokenContract
}

// IsNativeStakeToken return true in case a native token is used for staking
func IsNativeStakeToken(stakeTokenAddr types.Address) bool {
	return stakeTokenAddr == contracts.NativeERC20TokenContract
}
