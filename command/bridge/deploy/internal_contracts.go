//nolint:dupl
package deploy

import (
	"math/big"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/command"
	polycfg "github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
)

var bigZero = big.NewInt(0)

// initInternalContracts initializes the internal contracts
func initInternalContracts(chainCfg *chain.Chain) []*contract {
	useBridgeAllowList := chainCfg.Params.IsBridgeAllowListEnabled()
	useBridgeBlockList := chainCfg.Params.IsBridgeBlockListEnabled()

	internalContracts := make([]*contract, 0, 7)

	// Gateway contract
	internalContracts = append(internalContracts, &contract{
		name:     getContractName(true, gatewayName),
		hasProxy: true,
		artifact: contractsapi.Gateway,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.InternalGatewayAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			_ int64) error {
			validatorSet, err := getValidatorSet(fmt, genesisValidators)
			if err != nil {
				return err
			}

			inputParams := &contractsapi.InitializeGatewayFn{
				NewBls:     contracts.BLSContract,
				NewBn256G2: contracts.BLS256Contract,
				Validators: validatorSet,
			}

			return initContract(fmt, relayer, inputParams, config.ExternalGatewayAddr,
				gatewayName, key)
		},
	})

	// InternalERC20Predicate contract
	contractArtifact := contractsapi.ChildERC20Predicate
	if useBridgeAllowList || useBridgeBlockList {
		contractArtifact = contractsapi.ChildERC20PredicateACL
	}

	internalContracts = append(internalContracts, &contract{
		name:     getContractName(true, erc20PredicateName),
		hasProxy: true,
		artifact: contractArtifact,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.InternalERC20PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			var input contractsapi.FunctionAbi
			if useBridgeAllowList || useBridgeBlockList {
				input = &contractsapi.InitializeChildERC20PredicateACLFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewRootERC20Predicate:       config.ExternalERC20PredicateAddr,
					NewDestinationTokenTemplate: contracts.ChildERC20Contract,
					NewDestinationChainID:       big.NewInt(destinationChainID),
					NewNativeTokenRootAddress:   config.ExternalNativeERC20Addr,
					NewUseAllowList:             useBridgeAllowList,
					NewUseBlockList:             useBridgeBlockList,
					NewOwner:                    chainCfg.Params.GetBridgeOwner(),
				}
			} else {
				input = &contractsapi.InitializeChildERC20PredicateFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewRootERC20Predicate:       config.ExternalERC20PredicateAddr,
					NewDestinationTokenTemplate: contracts.ChildERC20Contract,
					NewNativeTokenRootAddress:   config.ExternalNativeERC20Addr,
					NewDestinationChainID:       big.NewInt(destinationChainID),
				}
			}

			return initContract(fmt, relayer, input, config.InternalERC20PredicateAddr,
				getContractName(true, erc20PredicateName), key)
		},
	})

	// InternalERC721Predicate contract
	contractArtifact = contractsapi.ChildERC721Predicate
	if useBridgeAllowList || useBridgeBlockList {
		contractArtifact = contractsapi.ChildERC721PredicateACL
	}

	internalContracts = append(internalContracts, &contract{
		name:     getContractName(true, erc721PredicateName),
		hasProxy: true,
		artifact: contractArtifact,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.InternalERC721PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			var input contractsapi.FunctionAbi
			if useBridgeAllowList || useBridgeBlockList {
				input = &contractsapi.InitializeChildERC721PredicateACLFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewRootERC721Predicate:      config.ExternalERC721PredicateAddr,
					NewDestinationTokenTemplate: contracts.ChildERC721Contract,
					NewDestinationChainID:       big.NewInt(destinationChainID),
					NewUseAllowList:             useBridgeAllowList,
					NewUseBlockList:             useBridgeBlockList,
					NewOwner:                    chainCfg.Params.GetBridgeOwner(),
				}
			} else {
				input = &contractsapi.InitializeChildERC721PredicateFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewRootERC721Predicate:      config.ExternalERC721PredicateAddr,
					NewDestinationTokenTemplate: contracts.ChildERC721Contract,
					NewDestinationChainID:       big.NewInt(destinationChainID),
				}
			}

			return initContract(fmt, relayer, input, config.InternalERC721PredicateAddr,
				getContractName(true, erc721PredicateName), key)
		},
	})

	// InternalERC1155Predicate contract
	contractArtifact = contractsapi.ChildERC1155Predicate
	if useBridgeAllowList || useBridgeBlockList {
		contractArtifact = contractsapi.ChildERC1155PredicateACL
	}

	internalContracts = append(internalContracts, &contract{
		name:     getContractName(true, erc1155PredicateName),
		hasProxy: true,
		artifact: contractArtifact,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.InternalERC1155PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			var input contractsapi.FunctionAbi
			if useBridgeAllowList || useBridgeBlockList {
				input = &contractsapi.InitializeChildERC1155PredicateACLFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewRootERC1155Predicate:     config.ExternalERC1155PredicateAddr,
					NewDestinationTokenTemplate: contracts.ChildERC1155Contract,
					NewDestinationChainID:       big.NewInt(destinationChainID),
					NewUseAllowList:             useBridgeAllowList,
					NewUseBlockList:             useBridgeBlockList,
					NewOwner:                    chainCfg.Params.GetBridgeOwner(),
				}
			} else {
				input = &contractsapi.InitializeChildERC1155PredicateFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewRootERC1155Predicate:     config.ExternalERC1155PredicateAddr,
					NewDestinationTokenTemplate: contracts.ChildERC1155Contract,
					NewDestinationChainID:       big.NewInt(destinationChainID),
				}
			}

			return initContract(fmt, relayer, input, config.InternalERC1155PredicateAddr,
				getContractName(true, erc1155PredicateName), key)
		},
	})

	// InternalMintableERC20Predicate contract
	contractArtifact = contractsapi.RootERC20Predicate
	if useBridgeAllowList || useBridgeBlockList {
		contractArtifact = contractsapi.RootMintableERC20PredicateACL
	}

	internalContracts = append(internalContracts, &contract{
		name:     getContractName(true, erc20MintablePredicateName),
		hasProxy: true,
		artifact: contractArtifact,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.InternalMintableERC20PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			var input contractsapi.FunctionAbi
			if useBridgeAllowList || useBridgeBlockList {
				input = &contractsapi.InitializeRootMintableERC20PredicateACLFn{
					NewGateway:             config.InternalGatewayAddr,
					NewChildERC20Predicate: config.ExternalMintableERC20PredicateAddr,
					NewTokenTemplate:       config.ExternalERC20Addr,
					NewDestinationChainID:  big.NewInt(destinationChainID),
					NewUseAllowList:        useBridgeAllowList,
					NewUseBlockList:        useBridgeBlockList,
					NewOwner:               chainCfg.Params.GetBridgeOwner(),
				}
			} else {
				input = &contractsapi.InitializeRootERC20PredicateFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewChildERC20Predicate:      config.ExternalMintableERC20PredicateAddr,
					NewDestinationTokenTemplate: config.ExternalERC20Addr,
					NewDestinationChainID:       big.NewInt(destinationChainID),
				}
			}

			return initContract(fmt, relayer, input, config.InternalMintableERC20PredicateAddr,
				getContractName(true, erc20MintablePredicateName), key)
		},
	})

	// InternalMintableERC721Predicate contract
	contractArtifact = contractsapi.RootERC721Predicate
	if useBridgeAllowList || useBridgeBlockList {
		contractArtifact = contractsapi.RootMintableERC721PredicateACL
	}

	internalContracts = append(internalContracts, &contract{
		name:     getContractName(true, erc721MintablePredicateName),
		hasProxy: true,
		artifact: contractArtifact,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.InternalMintableERC721PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			var input contractsapi.FunctionAbi
			if useBridgeAllowList || useBridgeBlockList {
				input = &contractsapi.InitializeRootMintableERC721PredicateACLFn{
					NewGateway:              config.InternalGatewayAddr,
					NewChildERC721Predicate: config.ExternalMintableERC721PredicateAddr,
					NewTokenTemplate:        config.ExternalERC721Addr,
					NewDestinationChainID:   big.NewInt(destinationChainID),
					NewUseAllowList:         useBridgeAllowList,
					NewUseBlockList:         useBridgeBlockList,
					NewOwner:                chainCfg.Params.GetBridgeOwner(),
				}
			} else {
				input = &contractsapi.InitializeRootERC721PredicateFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewChildERC721Predicate:     config.ExternalMintableERC721PredicateAddr,
					NewDestinationTokenTemplate: config.ExternalERC721Addr,
					NewDestinationChainID:       big.NewInt(destinationChainID),
				}
			}

			return initContract(fmt, relayer, input, config.InternalMintableERC721PredicateAddr,
				getContractName(true, erc721MintablePredicateName), key)
		},
	})

	// InternalMintableERC1155Predicate contract
	contractArtifact = contractsapi.RootERC1155Predicate
	if useBridgeAllowList || useBridgeBlockList {
		contractArtifact = contractsapi.RootMintableERC1155PredicateACL
	}

	internalContracts = append(internalContracts, &contract{
		name:     getContractName(true, erc1155MintablePredicateName),
		hasProxy: true,
		artifact: contractArtifact,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.InternalMintableERC1155PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			var input contractsapi.FunctionAbi
			if useBridgeAllowList || useBridgeBlockList {
				input = &contractsapi.InitializeRootMintableERC1155PredicateACLFn{
					NewGateway:               config.InternalGatewayAddr,
					NewChildERC1155Predicate: config.ExternalMintableERC1155PredicateAddr,
					NewTokenTemplate:         config.ExternalERC1155Addr,
					NewDestinationChainID:    big.NewInt(destinationChainID),
					NewUseAllowList:          useBridgeAllowList,
					NewUseBlockList:          useBridgeBlockList,
					NewOwner:                 chainCfg.Params.GetBridgeOwner(),
				}
			} else {
				input = &contractsapi.InitializeRootERC1155PredicateFn{
					NewGateway:                  config.InternalGatewayAddr,
					NewChildERC1155Predicate:    config.ExternalMintableERC1155PredicateAddr,
					NewDestinationTokenTemplate: config.ExternalERC1155Addr,
					NewDestinationChainID:       big.NewInt(destinationChainID),
				}
			}

			return initContract(fmt, relayer, input, config.InternalMintableERC1155PredicateAddr,
				getContractName(true, erc1155MintablePredicateName), key)
		},
	})

	return internalContracts
}

// preAllocateInternalPredicates pre-allocates internal predicates in genesis
// if the command is run in bootstrap mode
func preAllocateInternalPredicates(o command.OutputFormatter, internalContracts []*contract,
	chainCfg *chain.Chain, bridgeCfg *polycfg.Bridge) error {
	predicateBaseProxyAddress := contracts.ChildBridgeContractsBaseAddress

	if consensusCfg.Bridge != nil {
		for _, bridgeCfg := range consensusCfg.Bridge {
			highestAddr := bridgeCfg.GetHighestInternalAddress()
			if highestAddr.Compare(predicateBaseProxyAddress) > 0 {
				predicateBaseProxyAddress = highestAddr
			}
		}
	}

	lastAddress := predicateBaseProxyAddress
	for _, contract := range internalContracts {
		proxyAddress, implAddress := generateInternalContractAndProxyAddress(lastAddress)

		contract.addressPopulatorFn(bridgeCfg, []*deployContractResult{
			newDeployContractsResult(contract.name, false, implAddress, ethgo.ZeroHash, 0),
			newDeployContractsResult(contract.constructProxyName(), true, proxyAddress, ethgo.ZeroHash, 0),
		})

		chainCfg.Genesis.Alloc[implAddress] = &chain.GenesisAccount{
			Balance: bigZero,
			Code:    contract.artifact.DeployedBytecode,
		}

		lastAddress = proxyAddress
	}

	if _, err := o.Write([]byte("[BRIDGE - DEPLOY] Internal predicates pre-allocated in bootstrap mode\n")); err != nil {
		return err
	}

	return nil
}

// generateInternalContractAndProxyAddress generates the internal contract and proxy addresses
func generateInternalContractAndProxyAddress(lastAddress types.Address) (types.Address, types.Address) {
	proxyAddress := lastAddress.IncrementBy(10)
	implAddress := lastAddress.IncrementBy(1)

	return proxyAddress, implAddress
}
