package genesis

import (
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	apexBridgeFlag             = "apex"
	apexBridgeFlagDefaultValue = true
	apexBridgeDescriptionFlag  = "turn off London fork and some other settings needed for apex bridge"
)

func getApexContracts() []*contractInfo {
	return []*contractInfo{
		// Apex contracts
		{
			artifact: contractsapi.Bridge,
			address:  contracts.BridgeAddr,
		},
		{
			artifact: contractsapi.ClaimsHelper,
			address:  contracts.ClaimsHelperAddr,
		},
		{
			artifact: contractsapi.Claims,
			address:  contracts.ClaimsAddr,
		},
		{
			artifact: contractsapi.SignedBatches,
			address:  contracts.SignedBatchesAddr,
		},
		{
			artifact: contractsapi.Slots,
			address:  contracts.SlotsAddr,
		},
		{
			artifact: contractsapi.UTXOsc,
			address:  contracts.UTXOscAddr,
		},
		{
			artifact: contractsapi.Validators,
			address:  contracts.ValidatorsAddr,
		},
	}
}

func getApexProxyAddresses() (retVal []types.Address) {
	apexProxyToImplAddrMap := contracts.GetApexProxyImplementationMapping()
	for address := range apexProxyToImplAddrMap {
		retVal = append(retVal, address)
	}

	return
}

func (p *genesisParams) processConfigApex(chainConfig *chain.Chain) {
	if !p.apexBridge {
		return
	}

	chainConfig.Params.Forks.RemoveFork(chain.Governance).RemoveFork(chain.London)
	chainConfig.Params.BurnContract = nil
}
