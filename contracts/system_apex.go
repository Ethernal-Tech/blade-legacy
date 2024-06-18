package contracts

import "github.com/0xPolygon/polygon-edge/types"

var (
	// Apex contracts

	// Address of Bridge proxy
	Bridge     = types.StringToAddress("0xABEF000000000000000000000000000000000000")
	BridgeAddr = types.StringToAddress("0xABEF000000000000000000000000000000000010")

	// Address of ClaimsHelper proxy
	ClaimsHelper     = types.StringToAddress("0xABEF000000000000000000000000000000000001")
	ClaimsHelperAddr = types.StringToAddress("0xABEF000000000000000000000000000000000011")

	// Address of Claims proxy
	Claims     = types.StringToAddress("0xABEF000000000000000000000000000000000002")
	ClaimsAddr = types.StringToAddress("0xABEF000000000000000000000000000000000012")

	// Address of SignedBatches proxy
	SignedBatches     = types.StringToAddress("0xABEF000000000000000000000000000000000003")
	SignedBatchesAddr = types.StringToAddress("0xABEF000000000000000000000000000000000013")

	// Address of Slots proxy
	Slots     = types.StringToAddress("0xABEF000000000000000000000000000000000004")
	SlotsAddr = types.StringToAddress("0xABEF000000000000000000000000000000000014")

	// Address of Validators proxy
	Validators     = types.StringToAddress("0xABEF000000000000000000000000000000000005")
	ValidatorsAddr = types.StringToAddress("0xABEF000000000000000000000000000000000015")
)

func GetApexProxyImplementationMapping() map[types.Address]types.Address {
	return map[types.Address]types.Address{
		Bridge:        BridgeAddr,
		ClaimsHelper:  ClaimsHelperAddr,
		Claims:        ClaimsAddr,
		SignedBatches: SignedBatchesAddr,
		Slots:         SlotsAddr,
		Validators:    ValidatorsAddr,
	}
}
