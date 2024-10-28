package deploy

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/command"
	polycfg "github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

// initExternalContracts initializes the external contracts
func initExternalContracts(bridgeCfg *polycfg.Bridge,
	externalChainClient *jsonrpc.EthClient, externalChainID *big.Int) ([]*contract, error) {
	externalContracts := make([]*contract, 0)

	// deploy root ERC20 token only if non-mintable native token flavor is used on a child chain
	// and this external chain is the native token origin
	if !consensusCfg.NativeTokenConfig.IsMintable && consensusCfg.NativeTokenConfig.ChainID == externalChainID.Uint64() {
		if params.rootERC20TokenAddr != "" {
			// use existing root chain ERC20 token
			if err := populateExistingNativeTokenAddr(externalChainClient,
				params.rootERC20TokenAddr, erc20Name, bridgeCfg); err != nil {
				return nil, err
			}
		} else {
			// deploy MockERC20 as a root chain root native token
			externalContracts = append(externalContracts, &contract{
				name:     getContractName(false, erc20Name),
				hasProxy: false,
				artifact: contractsapi.RootERC20,
				addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
					bc.ExternalNativeERC20Addr = dcr[0].Address
				},
			})
		}
	}

	// BLS contract
	externalContracts = append(externalContracts, &contract{
		name:     blsName,
		hasProxy: true,
		artifact: contractsapi.BLS,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.BLSAddress = dcr[1].Address
		},
	})

	// BN256G2 contract
	externalContracts = append(externalContracts, &contract{
		name:     bn256G2Name,
		hasProxy: true,
		artifact: contractsapi.BLS256,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.BN256G2Address = dcr[1].Address
		},
	})

	// Gateway contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, gatewayName),
		hasProxy: true,
		artifact: contractsapi.Gateway,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalGatewayAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			if !consensusCfg.NativeTokenConfig.IsMintable {
				// we can not initialize Gateway contract at this moment if native token is not mintable
				// we will do that on finalize command when validators do premine and stake on BladeManager
				// this is done like this because Gateway contract needs to have correct
				// voting powers in order to correctly validate batches
				return nil
			}

			validatorSet, err := getValidatorSet(fmt, genesisValidators)
			if err != nil {
				return err
			}

			inputParams := &contractsapi.InitializeGatewayFn{
				NewBls:     config.BLSAddress,
				NewBn256G2: config.BN256G2Address,
				Validators: validatorSet,
			}

			return initContract(fmt, relayer, inputParams, config.ExternalGatewayAddr,
				gatewayName, key)
		},
	})

	// External ERC20Predicate contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc20PredicateName),
		hasProxy: true,
		artifact: contractsapi.RootERC20Predicate,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalERC20PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			input := &contractsapi.InitializeRootERC20PredicateFn{
				NewGateway:                  config.ExternalGatewayAddr,
				NewChildERC20Predicate:      config.InternalERC20PredicateAddr,
				NewDestinationTokenTemplate: contracts.ChildERC20Contract,
				// root native token address should be non-zero only if native token is non-mintable on a internal chain
				NewNativeTokenRoot:    config.ExternalNativeERC20Addr,
				NewDestinationChainID: big.NewInt(destinationChainID),
			}

			return initContract(fmt, relayer, input, config.ExternalERC20PredicateAddr,
				getContractName(false, erc20PredicateName), key)
		},
	})

	// External ERC20Mintable contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc20MintablePredicateName),
		hasProxy: true,
		artifact: contractsapi.ChildERC20Predicate,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalMintableERC20PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			input := &contractsapi.InitializeChildERC20PredicateFn{
				NewGateway:                  config.ExternalGatewayAddr,
				NewRootERC20Predicate:       config.InternalMintableERC20PredicateAddr,
				NewDestinationTokenTemplate: config.ExternalERC20Addr,
				NewDestinationChainID:       big.NewInt(destinationChainID),
			}

			return initContract(fmt, relayer, input, config.ExternalMintableERC20PredicateAddr,
				getContractName(false, erc20MintablePredicateName), key)
		},
	})

	// External ERC20Template contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc20TemplateName),
		hasProxy: false,
		artifact: contractsapi.ChildERC20,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalERC20Addr = dcr[0].Address
		},
	})

	// External ERC721Predicate contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc721PredicateName),
		hasProxy: true,
		artifact: contractsapi.RootERC721Predicate,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalERC721PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			input := &contractsapi.InitializeRootERC721PredicateFn{
				NewGateway:                  config.ExternalGatewayAddr,
				NewChildERC721Predicate:     config.InternalERC721PredicateAddr,
				NewDestinationTokenTemplate: contracts.ChildERC721Contract,
				NewDestinationChainID:       big.NewInt(destinationChainID),
			}

			return initContract(fmt, relayer, input, config.ExternalERC721PredicateAddr,
				getContractName(false, erc721PredicateName), key)
		},
	})

	// External ERC721MintablePredicate contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc721MintablePredicateName),
		hasProxy: true,
		artifact: contractsapi.ChildERC721Predicate,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalMintableERC721PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			input := &contractsapi.InitializeChildERC721PredicateFn{
				NewGateway:                  config.ExternalGatewayAddr,
				NewRootERC721Predicate:      config.InternalMintableERC721PredicateAddr,
				NewDestinationTokenTemplate: config.ExternalERC721Addr,
				NewDestinationChainID:       big.NewInt(destinationChainID),
			}

			return initContract(fmt, relayer, input, config.ExternalMintableERC721PredicateAddr,
				getContractName(false, erc721MintablePredicateName), key)
		},
	})

	// External ERC721Template contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc721TemplateName),
		hasProxy: false,
		artifact: contractsapi.ChildERC721,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalERC721Addr = dcr[0].Address
		},
	})

	// External ERC1155Predicate contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc1155PredicateName),
		hasProxy: true,
		artifact: contractsapi.RootERC1155Predicate,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalERC1155PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			input := &contractsapi.InitializeRootERC1155PredicateFn{
				NewGateway:                  config.ExternalGatewayAddr,
				NewChildERC1155Predicate:    config.InternalERC1155PredicateAddr,
				NewDestinationTokenTemplate: contracts.ChildERC1155Contract,
				NewDestinationChainID:       big.NewInt(destinationChainID),
			}

			return initContract(fmt, relayer, input, config.ExternalERC1155PredicateAddr,
				getContractName(false, erc1155PredicateName), key)
		},
	})

	// External ERC1155MintablePredicate contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc1155MintablePredicateName),
		hasProxy: true,
		artifact: contractsapi.ChildERC1155Predicate,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalMintableERC1155PredicateAddr = dcr[1].Address
		},
		initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
			genesisValidators []*validator.GenesisValidator,
			config *polycfg.Bridge,
			key crypto.Key,
			destinationChainID int64) error {
			input := &contractsapi.InitializeChildERC1155PredicateFn{
				NewGateway:                  config.ExternalGatewayAddr,
				NewRootERC1155Predicate:     config.InternalMintableERC1155PredicateAddr,
				NewDestinationTokenTemplate: config.ExternalERC1155Addr,
				NewDestinationChainID:       big.NewInt(destinationChainID),
			}

			return initContract(fmt, relayer, input, config.ExternalMintableERC1155PredicateAddr,
				getContractName(false, erc1155MintablePredicateName), key)
		},
	})

	// External ERC1155Template contract
	externalContracts = append(externalContracts, &contract{
		name:     getContractName(false, erc1155TemplateName),
		hasProxy: false,
		artifact: contractsapi.ChildERC1155,
		addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
			bc.ExternalERC1155Addr = dcr[0].Address
		},
	})

	if !consensusCfg.NativeTokenConfig.IsMintable {
		// if token is non-mintable we will deploy BladeManager
		// if not, we don't need it
		externalContracts = append(externalContracts, &contract{
			name:     bladeManagerName,
			artifact: contractsapi.BladeManager,
			hasProxy: true,
			addressPopulatorFn: func(bc *polycfg.Bridge, dcr []*deployContractResult) {
				bc.BladeManagerAddr = dcr[1].Address
			},
			initializeFn: func(fmt command.OutputFormatter, relayer txrelayer.TxRelayer,
				genesisValidators []*validator.GenesisValidator,
				config *polycfg.Bridge,
				key crypto.Key,
				destinationChainID int64) error {
				gvs := make([]*contractsapi.GenesisAccount, len(genesisValidators))
				for i := 0; i < len(genesisValidators); i++ {
					gvs[i] = &contractsapi.GenesisAccount{
						Addr:        genesisValidators[i].Address,
						IsValidator: true,
						// this is set on purpose to 0, each account must premine enough tokens to itself if token is non-mintable
						StakedTokens:   big.NewInt(0),
						PreminedTokens: big.NewInt(0),
					}
				}

				initParams := &contractsapi.InitializeBladeManagerFn{
					NewRootERC20Predicate: config.ExternalERC20PredicateAddr,
					GenesisAccounts:       gvs,
				}

				return initContract(fmt, relayer, initParams,
					config.BladeManagerAddr, bladeManagerName, key)
			},
		})
	}

	return externalContracts, nil
}

// populateExistingNativeTokenAddr checks whether given token is deployed on the provided address.
// If it is, then its address is set to the bridge config, otherwise an error is returned
func populateExistingNativeTokenAddr(eth *jsonrpc.EthClient, tokenAddr, tokenName string,
	bridgeCfg *polycfg.Bridge) error {
	addr := types.StringToAddress(tokenAddr)

	code, err := eth.GetCode(addr, jsonrpc.LatestBlockNumberOrHash)
	if err != nil {
		return fmt.Errorf("failed to check is %s token deployed: %w", tokenName, err)
	} else if code == "0x" {
		return fmt.Errorf("%s token is not deployed on provided address %s", tokenName, tokenAddr)
	}

	bridgeCfg.ExternalNativeERC20Addr = addr

	return nil
}
