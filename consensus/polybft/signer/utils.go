package signer

import (
	"bytes"
	"math/big"

	"github.com/Ethernal-Tech/ethgo/abi"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	addressABIType = abi.MustNewType("address")
	uint256ABIType = abi.MustNewType("uint256")
)

const (
	DomainValidatorSetString  = "DOMAIN_CHILD_VALIDATOR_SET"
	DomainBlockMetaDataString = "DOMAIN_BRIDGE_META"
	DomainCommonSigningString = "DOMAIN_COMMON_SIGNING"
	DomainStateReceiverString = "DOMAIN_STATE_RECEIVER"
)

var (
	// domain used to map hash to G1 used by (child) validator set
	DomainValidatorSet = crypto.Keccak256([]byte(DomainValidatorSetString))

	// domain used to map hash to G1 used by child checkpoint manager
	DomainBlockMeta = crypto.Keccak256([]byte(DomainBlockMetaDataString))

	DomainCommonSigning = crypto.Keccak256([]byte(DomainCommonSigningString))
	DomainStateReceiver = crypto.Keccak256([]byte(DomainStateReceiverString))
)

// MakeKOSKSignature creates KOSK signature which prevents rogue attack
func MakeKOSKSignature(privateKey *bls.PrivateKey, address types.Address,
	chainID int64, domain []byte, stakeManagerAddr types.Address) (*bls.Signature, error) {
	spenderABI, err := addressABIType.Encode(address)
	if err != nil {
		return nil, err
	}

	supernetManagerABI, err := addressABIType.Encode(stakeManagerAddr)
	if err != nil {
		return nil, err
	}

	chainIDABI, err := uint256ABIType.Encode(big.NewInt(chainID))
	if err != nil {
		return nil, err
	}

	// ethgo pads address to 32 bytes, but solidity doesn't (keeps it 20 bytes)
	// that's why we are skipping first 12 bytes
	message := bytes.Join([][]byte{spenderABI[12:], supernetManagerABI[12:], chainIDABI}, nil)

	return privateKey.Sign(message, domain)
}
