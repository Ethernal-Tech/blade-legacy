package contractsapi

import (
	"github.com/0xPolygon/polygon-edge/contracts"
)

var (
	// Apex smart contracts
	Bridge        *contracts.Artifact
	ClaimsHelper  *contracts.Artifact
	Claims        *contracts.Artifact
	SignedBatches *contracts.Artifact
	Slots         *contracts.Artifact
	Validators    *contracts.Artifact
)

func initApexContracts() (
	bridge *contracts.Artifact,
	claimsHelper *contracts.Artifact,
	claims *contracts.Artifact,
	signedBatches *contracts.Artifact,
	slots *contracts.Artifact,
	validators *contracts.Artifact,
	err error) {
	bridge, err = contracts.DecodeArtifact([]byte(BridgeArtifact))
	if err != nil {
		return
	}

	claimsHelper, err = contracts.DecodeArtifact([]byte(ClaimsHelperArtifact))
	if err != nil {
		return
	}

	claims, err = contracts.DecodeArtifact([]byte(ClaimsArtifact))
	if err != nil {
		return
	}

	signedBatches, err = contracts.DecodeArtifact([]byte(SignedBatchesArtifact))
	if err != nil {
		return
	}

	slots, err = contracts.DecodeArtifact([]byte(SlotsArtifact))
	if err != nil {
		return
	}

	validators, err = contracts.DecodeArtifact([]byte(ValidatorsArtifact))
	if err != nil {
		return
	}

	return
}
