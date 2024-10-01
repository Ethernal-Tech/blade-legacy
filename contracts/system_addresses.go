package contracts

import "github.com/0xPolygon/polygon-edge/types"

var (
	// EpochManagerContract is an address of validator set proxy contract deployed to child chain
	EpochManagerContract = types.StringToAddress("0x101")
	// EpochManagerContractV1 is an address of validator set implementation contract deployed to child chain
	EpochManagerContractV1 = types.StringToAddress("0x1011")
	// BLSContract is an address of BLS proxy contract on the child chain
	BLSContract = types.StringToAddress("0x102")
	// BLSContractV1 is an address of BLS contract on the child chain
	BLSContractV1 = types.StringToAddress("0x1021")
	// RewardTokenContract is an address of reward token proxy on child chain
	RewardTokenContract = types.StringToAddress("0x103")
	// RewardTokenContractV1 is an address of reward token on child chain
	RewardTokenContractV1 = types.StringToAddress("0x1031")
	// BLS256Contract is an address of BLS256 proxy contract on the child chain
	BLS256Contract = types.StringToAddress("0x104")
	// BLS256ContractV1 is an address of BLS256 contract on the child chain
	BLS256ContractV1 = types.StringToAddress("0x1041")
	// DefaultBurnContract is an address of eip1559 default proxy contract
	DefaultBurnContract = types.StringToAddress("0x105")
	// NativeERC20TokenContract is an address of bridge proxy contract
	// (used for transferring ERC20 native tokens on child chain)
	NativeERC20TokenContract = types.StringToAddress("0x106")
	// NativeERC20TokenContractV1 is an address of bridge contract
	// (used for transferring ERC20 native tokens on child chain)
	NativeERC20TokenContractV1 = types.StringToAddress("0x1061")
	// StakeManagerContract is an address of stake manager proxy contract on child chain
	StakeManagerContract = types.StringToAddress("0x107")
	// StakeManagerContract is an address of stake manager contract on child chain
	StakeManagerContractV1 = types.StringToAddress("0x1071")
	// BridgeStorageContract is an address of bridge storage proxy contract
	BridgeStorageContract = types.StringToAddress("0x108")
	// BridgeStorageContractV1 is an address of bridge storage contract on child chain
	BridgeStorageContractV1 = types.StringToAddress("0x1081")

	// ChildERC20Contract is an address of ERC20 token template
	ChildERC20Contract = types.StringToAddress("0x1003")
	// ChildERC721Contract is an address of ERC721 token template
	ChildERC721Contract = types.StringToAddress("0x1004")
	// ChildERC1155Contract is an address of ERC1155 token template
	ChildERC1155Contract = types.StringToAddress("0x1005")

	ChildBridgeContractsBaseAddress = types.StringToAddress("0x5006")

	// Governance contracts ===============================================================================

	// ChildGovernorContract is the proxy address of main governance contract
	ChildGovernorContract = types.StringToAddress("0x10a")
	// ChildGovernorContract is an address of main governance contract
	ChildGovernorContractV1 = types.StringToAddress("0x10a1")
	// ChildTimelockContract is the proxy address of timelock contract used by the governor contract
	ChildTimelockContract = types.StringToAddress("0x10b")
	// ChildTimelockContract is an address of timelock contract used by the governor contract
	ChildTimelockContractV1 = types.StringToAddress("0x10b1")
	// NetworkParamsContract is the proxy address of NetworkParams contract which holds network config params
	NetworkParamsContract = types.StringToAddress("0x10c")
	// NetworkParamsContract is an address of NetworkParams contract which holds network config params
	NetworkParamsContractV1 = types.StringToAddress("0x10c1")
	// ForkParamsContract is an address of ForkParams contract which holds data of enabled forks
	ForkParamsContract = types.StringToAddress("0x10d")
	// ForkParamsContract is the proxy address of ForkParams contract which holds data of enabled forks
	ForkParamsContractV1 = types.StringToAddress("0x10d1")

	// SystemCaller is address of account, used for system calls to smart contracts
	SystemCaller = types.StringToAddress("0xffffFFFfFFffffffffffffffFfFFFfffFFFfFFfE")

	// NativeTransferPrecompile is an address of native transfer precompile
	NativeTransferPrecompile = types.StringToAddress("0x2020")
	// ConsolePrecompile is and address of Hardhat console precompile
	ConsolePrecompile = types.StringToAddress("0x000000000000000000636F6e736F6c652e6c6f67")
	// AllowListContractsAddr is the address of the contract deployer allow list
	AllowListContractsAddr = types.StringToAddress("0x0200000000000000000000000000000000000000")
	// BlockListContractsAddr is the address of the contract deployer block list
	BlockListContractsAddr = types.StringToAddress("0x0300000000000000000000000000000000000000")
	// AllowListTransactionsAddr is the address of the transactions allow list
	AllowListTransactionsAddr = types.StringToAddress("0x0200000000000000000000000000000000000002")
	// BlockListTransactionsAddr is the address of the transactions block list
	BlockListTransactionsAddr = types.StringToAddress("0x0300000000000000000000000000000000000002")
	// AllowListBridgeAddr is the address of the bridge allow list
	AllowListBridgeAddr = types.StringToAddress("0x0200000000000000000000000000000000000004")
	// BlockListBridgeAddr is the address of the bridge block list
	BlockListBridgeAddr = types.StringToAddress("0x0300000000000000000000000000000000000004")
)

// GetProxyImplementationMapping retrieves the addresses of proxy contracts that should be deployed unconditionally
func GetProxyImplementationMapping() map[types.Address]types.Address {
	return map[types.Address]types.Address{
		BridgeStorageContract:    BridgeStorageContractV1,
		BLSContract:              BLSContractV1,
		EpochManagerContract:     EpochManagerContractV1,
		StakeManagerContract:     StakeManagerContractV1,
		NativeERC20TokenContract: NativeERC20TokenContractV1,
		NetworkParamsContract:    NetworkParamsContractV1,
		ForkParamsContract:       ForkParamsContractV1,
		ChildTimelockContract:    ChildTimelockContractV1,
		ChildGovernorContract:    ChildGovernorContractV1,
		BLS256Contract:           BLS256ContractV1,
	}
}
