package polybft

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	maxNumberOfTransactions = 5
	timeoutBlocksNumber     = 5
)

func initApex(transition *state.Transition, admin types.Address,
	polyBFTConfig PolyBFTConfig) (err error) {
	// Initialize Apex proxies
	if err = initApexProxies(transition, admin,
		contracts.GetApexProxyImplementationMapping(), polyBFTConfig); err != nil {
		return err
	}

	// Initialize Apex contracts

	if err = initBridge(transition); err != nil {
		return err
	}

	if err = initSignedBatches(transition); err != nil {
		return err
	}

	if err = initClaimsHelper(transition); err != nil {
		return err
	}

	if err = initValidators(transition); err != nil {
		return err
	}

	if err = initSlots(transition); err != nil {
		return err
	}

	if err = initClaims(transition); err != nil {
		return err
	}

	return nil
}

// initProxies initializes proxy contracts, that allow upgradeability of contracts implementation
func initApexProxies(transition *state.Transition, admin types.Address,
	proxyToImplMap map[types.Address]types.Address, polyBFTConfig PolyBFTConfig) error {
	for proxyAddress, implAddress := range proxyToImplMap {
		protectSetupProxyFn := &contractsapi.ProtectSetUpProxyGenesisProxyFn{Initiator: contracts.SystemCaller}

		proxyInput, err := protectSetupProxyFn.EncodeAbi()
		if err != nil {
			return fmt.Errorf("GenesisProxy.protectSetUpProxy params encoding failed: %w", err)
		}

		err = callContract(contracts.SystemCaller, proxyAddress, proxyInput, "GenesisProxy.protectSetUpProxy", transition)
		if err != nil {
			return err
		}

		data, err := getDataForApexContract(proxyAddress, polyBFTConfig)
		if err != nil {
			return fmt.Errorf("initialize encoding for %v proxy failed: %w", proxyAddress, err)
		}

		setUpproxyFn := &contractsapi.SetUpProxyGenesisProxyFn{
			Logic: implAddress,
			Admin: admin,
			Data:  data,
		}

		proxyInput, err = setUpproxyFn.EncodeAbi()
		if err != nil {
			return fmt.Errorf("apex GenesisProxy.setUpProxy params encoding failed: %w", err)
		}

		err = callContract(contracts.SystemCaller, proxyAddress, proxyInput, "GenesisProxy.setUpProxy", transition)
		if err != nil {
			return err
		}
	}

	return nil
}

func getDataForApexContract(contract types.Address, polyBFTConfig PolyBFTConfig) ([]byte, error) {
	switch contract {
	case contracts.Bridge:
		return (&contractsapi.InitializeBridgeFn{}).EncodeAbi()
	case contracts.SignedBatches:
		return (&contractsapi.InitializeSignedBatchesFn{}).EncodeAbi()
	case contracts.ClaimsHelper:
		return (&contractsapi.InitializeClaimsHelperFn{}).EncodeAbi()
	case contracts.Validators:
		var validatorAddresses = make([]types.Address, len(polyBFTConfig.InitialValidatorSet))
		for i, validator := range polyBFTConfig.InitialValidatorSet {
			validatorAddresses[i] = validator.Address
		}

		return (&contractsapi.InitializeValidatorsFn{
			Validators: validatorAddresses,
		}).EncodeAbi()
	case contracts.Slots:
		return (&contractsapi.InitializeSlotsFn{}).EncodeAbi()
	case contracts.Claims:
		return (&contractsapi.InitializeClaimsFn{
			MaxNumberOfTransactions: maxNumberOfTransactions,
			TimeoutBlocksNumber:     timeoutBlocksNumber,
		}).EncodeAbi()
	default:
		return nil, fmt.Errorf("no contract defined at address %v", contract)
	}
}

// Apex smart contracts initialization

// initBridge initializes Bridge and it's proxy SC
func initBridge(transition *state.Transition) error {
	setDependenciesFn := &contractsapi.SetDependenciesBridgeFn{
		ClaimsAddress:        contracts.Claims,
		SignedBatchesAddress: contracts.SignedBatches,
		SlotsAddress:         contracts.Slots,
		ValidatorsAddress:    contracts.Validators,
	}

	input, err := setDependenciesFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("Bridge.setDependencies params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.Bridge, input, "Bridge.setDependencies", transition)
}

// initSignedBatches initializes SignedBatches SC
func initSignedBatches(transition *state.Transition) error {
	setDependenciesFn := &contractsapi.SetDependenciesSignedBatchesFn{
		BridgeAddress:       contracts.Bridge,
		ClaimsHelperAddress: contracts.ClaimsHelper,
		ValidatorsAddress:   contracts.Validators,
	}

	input, err := setDependenciesFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("SignedBatches.setDependencies params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.SignedBatches, input, "SignedBatches.setDependencies", transition)
}

// initClaimsHelper initializes ClaimsHelper SC
func initClaimsHelper(transition *state.Transition) error {
	setDependenciesFn := &contractsapi.SetDependenciesClaimsHelperFn{
		ClaimsAddress:        contracts.Claims,
		SignedBatchesAddress: contracts.SignedBatches,
	}

	input, err := setDependenciesFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("ClaimsHelper.setDependencies params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.ClaimsHelper, input, "ClaimsHelper.setDependencies", transition)
}

// initValidators initializes Validators SC
func initValidators(transition *state.Transition) error {
	setDependenciesFn := &contractsapi.SetDependenciesValidatorsFn{
		BridgeAddress: contracts.Bridge,
	}

	input, err := setDependenciesFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("Validators.setDependencies params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.Validators, input, "Validators.setDependencies", transition)
}

// initSlots initializes Slots SC
func initSlots(transition *state.Transition) error {
	setDependenciesFn := &contractsapi.SetDependenciesSlotsFn{
		BridgeAddress:     contracts.Bridge,
		ValidatorsAddress: contracts.Validators,
	}

	input, err := setDependenciesFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("Slots.setDependencies params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.Slots, input, "Slots.setDependencies", transition)
}

// initClaims initializes Claims SC
func initClaims(transition *state.Transition) error {
	setDependenciesFn := &contractsapi.SetDependenciesClaimsFn{
		BridgeAddress:       contracts.Bridge,
		ClaimsHelperAddress: contracts.ClaimsHelper,
		ValidatorsAddress:   contracts.Validators,
	}

	input, err := setDependenciesFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("Claims.setDependencies params encoding failed: %w", err)
	}

	return callContract(contracts.SystemCaller,
		contracts.Claims, input, "Claims.setDependencies", transition)
}
