package deploy

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/bridge/helper"
	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	contractsDeploymentTitle = "[BRIDGE - CONTRACTS DEPLOYMENT]"

	proxySufix     = "Proxy"
	externalPrefix = "External"
	internalPrefix = "Internal"

	gatewayName                  = "Gateway"
	bladeManagerName             = "BladeManager"
	blsName                      = "BLS"
	bn256G2Name                  = "BN256G2"
	erc20PredicateName           = "ERC20Predicate"
	erc20MintablePredicateName   = "ERC20MintablePredicate"
	erc20Name                    = "ERC20"
	erc20TemplateName            = "ERC20Template"
	erc721PredicateName          = "ERC721Predicate"
	erc721MintablePredicateName  = "ERC721MintablePredicate"
	erc721TemplateName           = "ERC721Template"
	erc1155PredicateName         = "ERC1155Predicate"
	erc1155MintablePredicateName = "ERC1155MintablePredicate"
	erc1155TemplateName          = "ERC1155Template"
)

type addressPopulator func(*polybft.BridgeConfig, []*deployContractResult)
type initializer func(command.OutputFormatter, txrelayer.TxRelayer,
	[]*validator.GenesisValidator,
	*polybft.BridgeConfig, crypto.Key, int64) error

// contract represents a contract to be deployed
type contract struct {
	name               string
	hasProxy           bool
	artifact           *contracts.Artifact
	addressPopulatorFn addressPopulator
	initializeFn       initializer
}

// deploy deploys the contract and its proxy if it has one, and returns the deployment results
func (c *contract) deploy(
	bridgeCfg *polybft.BridgeConfig,
	txRelayer txrelayer.TxRelayer, deployerKey crypto.Key,
	proxyAdmin types.Address) ([]*deployContractResult, error) {
	txn := helper.CreateTransaction(types.ZeroAddress, nil, c.artifact.Bytecode, nil, true)

	receipt, err := txRelayer.SendTransaction(txn, deployerKey)
	if err != nil {
		return nil, fmt.Errorf("failed sending %s contract deploy transaction: %w", c.name, err)
	}

	if receipt == nil || receipt.Status != uint64(types.ReceiptSuccess) {
		return nil, fmt.Errorf("deployment of %s contract failed", c.name)
	}

	deployResults := make([]*deployContractResult, 0, 2)
	implementationAddress := types.Address(receipt.ContractAddress)

	deployResults = append(deployResults, newDeployContractsResult(c.name, false,
		implementationAddress,
		receipt.TransactionHash,
		receipt.GasUsed))

	if c.hasProxy {
		proxyContractName := getProxyNameForImpl(c.name)

		receipt, err := helper.DeployProxyContract(
			txRelayer, deployerKey, proxyContractName, proxyAdmin, implementationAddress)
		if err != nil {
			return nil, err
		}

		if receipt == nil || receipt.Status != uint64(types.ReceiptSuccess) {
			return nil, fmt.Errorf("deployment of %s contract failed", proxyContractName)
		}

		deployResults = append(deployResults, newDeployContractsResult(proxyContractName, true,
			types.Address(receipt.ContractAddress),
			receipt.TransactionHash,
			receipt.GasUsed))
	}

	c.addressPopulatorFn(bridgeCfg, deployResults)

	return deployResults, nil
}

func getProxyNameForImpl(input string) string {
	return input + proxySufix
}

func getContractName(isInternal bool, input string) string {
	prefix := externalPrefix
	if isInternal {
		prefix = internalPrefix
	}

	return prefix + input + proxySufix
}
