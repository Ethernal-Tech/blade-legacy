package polybft

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo/abi"
	"github.com/hashicorp/go-hclog"
	"github.com/umbracle/fastrlp"
)

const (
	// ExtraVanity represents a fixed number of extra-data bytes reserved for proposer vanity
	ExtraVanity = 32

	// ExtraSeal represents the fixed number of extra-data bytes reserved for proposer seal
	ExtraSeal = 65
)

// PolyBFTMixDigest represents a hash of "PolyBFT Mix" to identify whether the block is from PolyBFT consensus engine
var PolyBFTMixDigest = types.StringToHash("adce6e5230abe012342a44e4e9b6d05997d6f015387ae0e59be924afc7ec70c1")

// Extra defines the structure of the extra field for Istanbul
type Extra struct {
	Validators    *validator.ValidatorSetDelta
	Parent        *Signature
	Committed     *Signature
	BlockMetaData *BlockMetaData
}

// MarshalRLPTo defines the marshal function wrapper for Extra
func (i *Extra) MarshalRLPTo(dst []byte) []byte {
	ar := &fastrlp.Arena{}

	return append(make([]byte, ExtraVanity), i.MarshalRLPWith(ar).MarshalTo(dst)...)
}

// MarshalRLPWith defines the marshal function implementation for Extra
func (i *Extra) MarshalRLPWith(ar *fastrlp.Arena) *fastrlp.Value {
	vv := ar.NewArray()

	// Validators
	if i.Validators == nil {
		vv.Set(ar.NewNullArray())
	} else {
		vv.Set(i.Validators.MarshalRLPWith(ar))
	}

	// Parent Signatures
	if i.Parent == nil {
		vv.Set(ar.NewNullArray())
	} else {
		vv.Set(i.Parent.MarshalRLPWith(ar))
	}

	// Committed Signatures
	if i.Committed == nil {
		vv.Set(ar.NewNullArray())
	} else {
		vv.Set(i.Committed.MarshalRLPWith(ar))
	}

	// Block Metadata
	if i.BlockMetaData == nil {
		vv.Set(ar.NewNullArray())
	} else {
		vv.Set(i.BlockMetaData.MarshalRLPWith(ar))
	}

	return vv
}

// UnmarshalRLP defines the unmarshal function wrapper for Extra
func (i *Extra) UnmarshalRLP(input []byte) error {
	return fastrlp.UnmarshalRLP(input[ExtraVanity:], i)
}

// UnmarshalRLPWith defines the unmarshal implementation for Extra
func (i *Extra) UnmarshalRLPWith(v *fastrlp.Value) error {
	const expectedElements = 4

	elems, err := v.GetElems()
	if err != nil {
		return err
	}

	if num := len(elems); num != expectedElements {
		return fmt.Errorf("incorrect elements count to decode Extra, expected %d but found %d", expectedElements, num)
	}

	// Validators
	if elems[0].Elems() > 0 {
		i.Validators = &validator.ValidatorSetDelta{}
		if err := i.Validators.UnmarshalRLPWith(elems[0]); err != nil {
			return err
		}
	}

	// Parent Signatures
	if elems[1].Elems() > 0 {
		i.Parent = &Signature{}
		if err := i.Parent.UnmarshalRLPWith(elems[1]); err != nil {
			return err
		}
	}

	// Committed Signatures
	if elems[2].Elems() > 0 {
		i.Committed = &Signature{}
		if err := i.Committed.UnmarshalRLPWith(elems[2]); err != nil {
			return err
		}
	}

	// Block Metadata
	if elems[3].Elems() > 0 {
		i.BlockMetaData = &BlockMetaData{}
		if err := i.BlockMetaData.UnmarshalRLPWith(elems[3]); err != nil {
			return err
		}
	}

	return nil
}

// ValidateFinalizedData contains extra data validations for finalized headers
func (i *Extra) ValidateFinalizedData(header *types.Header, parent *types.Header, parents []*types.Header,
	chainID uint64, consensusBackend polybftBackend, domain []byte, logger hclog.Logger) error {
	// validate committed signatures
	blockNumber := header.Number
	if i.Committed == nil {
		return fmt.Errorf("failed to verify signatures for block %d, because signatures are not present", blockNumber)
	}

	if i.BlockMetaData == nil {
		return fmt.Errorf("failed to verify signatures for block %d, because block meta data are not present", blockNumber)
	}

	// validate current block signatures
	blockMetaHash, err := i.BlockMetaData.Hash(header.Hash)
	if err != nil {
		return fmt.Errorf("failed to calculate proposal hash: %w", err)
	}

	validators, err := consensusBackend.GetValidators(blockNumber-1, parents)
	if err != nil {
		return fmt.Errorf("failed to validate header for block %d. could not retrieve block validators:%w", blockNumber, err)
	}

	if err := i.Committed.Verify(blockNumber, validators, blockMetaHash, domain, logger); err != nil {
		return fmt.Errorf("failed to verify signatures for block %d (proposal hash %s): %w",
			blockNumber, blockMetaHash, err)
	}

	parentExtra, err := GetIbftExtra(parent.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to verify signatures for block %d: %w", blockNumber, err)
	}

	// validate parent signatures
	if err := i.ValidateParentSignatures(blockNumber, consensusBackend, parents,
		parent, parentExtra, domain, logger); err != nil {
		return err
	}

	return i.BlockMetaData.Validate(parentExtra.BlockMetaData)
}

// ValidateParentSignatures validates signatures for parent block
func (i *Extra) ValidateParentSignatures(blockNumber uint64, consensusBackend polybftBackend, parents []*types.Header,
	parent *types.Header, parentExtra *Extra, domain []byte, logger hclog.Logger) error {
	// skip block 1 because genesis does not have committed signatures
	if blockNumber <= 1 {
		return nil
	}

	if i.Parent == nil {
		return fmt.Errorf("failed to verify signatures for parent of block %d because signatures are not present",
			blockNumber)
	}

	parentValidators, err := consensusBackend.GetValidators(blockNumber-2, parents)
	if err != nil {
		return fmt.Errorf(
			"failed to validate header for block %d. could not retrieve parent validators: %w",
			blockNumber,
			err,
		)
	}

	parentBlockMetaHash, err := parentExtra.BlockMetaData.Hash(parent.Hash)
	if err != nil {
		return fmt.Errorf("failed to calculate parent proposal hash: %w", err)
	}

	parentBlockNumber := blockNumber - 1
	if err := i.Parent.Verify(parentBlockNumber, parentValidators, parentBlockMetaHash, domain, logger); err != nil {
		return fmt.Errorf("failed to verify signatures for parent of block %d (proposal hash: %s): %w",
			blockNumber, parentBlockMetaHash, err)
	}

	return nil
}

// Signature represents aggregated signatures of signers accompanied with a bitmap
// (in order to be able to determine identities of each signer)
type Signature struct {
	AggregatedSignature []byte
	Bitmap              []byte
}

// MarshalRLPWith marshals Signature object into RLP format
func (s *Signature) MarshalRLPWith(ar *fastrlp.Arena) *fastrlp.Value {
	committed := ar.NewArray()
	if s.AggregatedSignature == nil {
		committed.Set(ar.NewNull())
	} else {
		committed.Set(ar.NewBytes(s.AggregatedSignature))
	}

	if s.Bitmap == nil {
		committed.Set(ar.NewNull())
	} else {
		committed.Set(ar.NewBytes(s.Bitmap))
	}

	return committed
}

// UnmarshalRLPWith unmarshals Signature object from the RLP format
func (s *Signature) UnmarshalRLPWith(v *fastrlp.Value) error {
	vals, err := v.GetElems()
	if err != nil {
		return fmt.Errorf("array type expected for signature struct")
	}

	// there should be exactly two elements (aggregated signature and bitmap)
	if num := len(vals); num != 2 {
		return fmt.Errorf("incorrect elements count to decode Signature, expected 2 but found %d", num)
	}

	s.AggregatedSignature, err = vals[0].GetBytes(nil)
	if err != nil {
		return err
	}

	s.Bitmap, err = vals[1].GetBytes(nil)
	if err != nil {
		return err
	}

	return nil
}

// Verify is used to verify aggregated signature based on current validator set, message hash and domain
func (s *Signature) Verify(blockNumber uint64, validators validator.AccountSet,
	hash types.Hash, domain []byte, logger hclog.Logger) error {
	signers, err := validators.GetFilteredValidators(s.Bitmap)
	if err != nil {
		return err
	}

	validatorSet := validator.NewValidatorSet(validators, logger)
	if !validatorSet.HasQuorum(blockNumber, signers.GetAddressesAsSet()) {
		return fmt.Errorf("quorum not reached")
	}

	blsPublicKeys := make([]*bls.PublicKey, len(signers))
	for i, validator := range signers {
		blsPublicKeys[i] = validator.BlsKey
	}

	aggs, err := bls.UnmarshalSignature(s.AggregatedSignature)
	if err != nil {
		return err
	}

	if !aggs.VerifyAggregated(blsPublicKeys, hash[:], domain) {
		return fmt.Errorf("could not verify aggregated signature")
	}

	return nil
}

var blockMetaDataABIType = abi.MustNewType(`tuple(
	bytes32 blockHash,
	uint256 blockRound, 
	uint256 epochNumber)`)

// BlockMetaData represents block meta data
type BlockMetaData struct {
	BlockRound  uint64
	EpochNumber uint64
}

// MarshalRLPWith defines the marshal function implementation for BlockMeta
func (c *BlockMetaData) MarshalRLPWith(ar *fastrlp.Arena) *fastrlp.Value {
	vv := ar.NewArray()
	// BlockRound
	vv.Set(ar.NewUint(c.BlockRound))
	// EpochNumber
	vv.Set(ar.NewUint(c.EpochNumber))

	return vv
}

// UnmarshalRLPWith unmarshals BlockMetaData object from the RLP format
func (c *BlockMetaData) UnmarshalRLPWith(v *fastrlp.Value) error {
	vals, err := v.GetElems()
	if err != nil {
		return fmt.Errorf("array type expected for BlockMetaData struct")
	}

	// there should be exactly 2 elements:
	// BlockRound, EpochNumber
	if num := len(vals); num != 2 {
		return fmt.Errorf("incorrect elements count to decode BlockMetaData, expected 2 but found %d", num)
	}

	// BlockRound
	c.BlockRound, err = vals[0].GetUint64()
	if err != nil {
		return err
	}

	// EpochNumber
	c.EpochNumber, err = vals[1].GetUint64()
	if err != nil {
		return err
	}

	return nil
}

// Copy returns deep copy of BlockMetaData instance
func (c *BlockMetaData) Copy() *BlockMetaData {
	newBlockMetaData := new(BlockMetaData)
	*newBlockMetaData = *c

	return newBlockMetaData
}

// Hash calculates keccak256 hash of the BlockMetaData.
// BlockMetaData is ABI encoded and then hashed.
func (c *BlockMetaData) Hash(blockHash types.Hash) (types.Hash, error) {
	blockMetaDataMap := map[string]interface{}{
		"blockHash":   blockHash,
		"blockRound":  new(big.Int).SetUint64(c.BlockRound),
		"epochNumber": new(big.Int).SetUint64(c.EpochNumber),
	}

	abiEncoded, err := blockMetaDataABIType.Encode(blockMetaDataMap)
	if err != nil {
		return types.ZeroHash, err
	}

	return types.BytesToHash(crypto.Keccak256(abiEncoded)), nil
}

// Validate encapsulates basic validation logic for block meta data.
// It only checks epoch numbers validity and whether validators hashes are non-empty.
func (c *BlockMetaData) Validate(parentBlockMetaData *BlockMetaData) error {
	if c.EpochNumber != parentBlockMetaData.EpochNumber &&
		c.EpochNumber != parentBlockMetaData.EpochNumber+1 {
		// epoch-beginning block
		// epoch number must be incremented by one compared to parent block's block
		return fmt.Errorf("invalid epoch number for epoch-beginning block")
	}

	return nil
}

// GetIbftExtraClean returns unmarshaled extra field from the passed in header,
// but without signatures for the given header (it only includes signatures for the parent block)
func GetIbftExtraClean(extraRaw []byte) ([]byte, error) {
	extra, err := GetIbftExtra(extraRaw)
	if err != nil {
		return nil, err
	}

	ibftExtra := &Extra{
		Parent:        extra.Parent,
		Validators:    extra.Validators,
		BlockMetaData: extra.BlockMetaData,
		Committed:     &Signature{},
	}

	return ibftExtra.MarshalRLPTo(nil), nil
}

// GetIbftExtra returns the istanbul extra data field from the passed in header
func GetIbftExtra(extraRaw []byte) (*Extra, error) {
	if len(extraRaw) < ExtraVanity {
		return nil, fmt.Errorf("wrong extra size: %d", len(extraRaw))
	}

	extra := &Extra{}

	if err := extra.UnmarshalRLP(extraRaw); err != nil {
		return nil, err
	}

	return extra, nil
}
