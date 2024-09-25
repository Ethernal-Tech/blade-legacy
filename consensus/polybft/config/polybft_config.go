package config

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	ConsensusName              = "polybft"
	minNativeTokenParamsNumber = 4

	defaultNativeTokenName     = "Polygon"
	defaultNativeTokenSymbol   = "MATIC"
	defaultNativeTokenDecimals = uint8(18)
)

var (
	DefaultTokenConfig = &Token{
		Name:       defaultNativeTokenName,
		Symbol:     defaultNativeTokenSymbol,
		Decimals:   defaultNativeTokenDecimals,
		IsMintable: true,
	}

	errInvalidTokenParams = errors.New("native token params were not submitted in proper format " +
		"(<name:symbol:decimals count:is minted on the local chain>)")
)

// PolyBFT is the configuration file for the Polybft consensus protocol.
type PolyBFT struct {
	// InitialValidatorSet are the genesis validators
	InitialValidatorSet []*validator.GenesisValidator `json:"initialValidatorSet"`

	// Bridge represent configuration for external bridges
	Bridge map[uint64]*Bridge `json:"bridge"`

	// EpochSize is size of epoch
	EpochSize uint64 `json:"epochSize"`

	// EpochReward is assigned to validators for blocks sealing
	EpochReward uint64 `json:"epochReward"`

	// SprintSize is size of sprint
	SprintSize uint64 `json:"sprintSize"`

	// BlockTime is target frequency of blocks production
	BlockTime common.Duration `json:"blockTime"`

	// Governance is the initial governance address
	Governance types.Address `json:"governance"`

	// NativeTokenConfig defines name, symbol and decimal count of the native token
	NativeTokenConfig *Token `json:"nativeTokenConfig"`

	// InitialTrieRoot corresponds to pre-existing state root in case data gets migrated from a legacy system
	InitialTrieRoot types.Hash `json:"initialTrieRoot"`

	// MinValidatorSetSize indicates the minimum size of validator set
	MinValidatorSetSize uint64 `json:"minValidatorSetSize"`

	// MaxValidatorSetSize indicates the maximum size of validator set
	MaxValidatorSetSize uint64 `json:"maxValidatorSetSize"`

	// CheckpointInterval indicates the number of blocks after which a new checkpoint is submitted
	CheckpointInterval uint64 `json:"checkpointInterval"`

	// WithdrawalWaitPeriod indicates a number of epochs after which withdrawal can be done from child chain
	WithdrawalWaitPeriod uint64 `json:"withdrawalWaitPeriod"`

	// RewardConfig defines rewards configuration
	RewardConfig *Rewards `json:"rewardConfig"`

	// BlockTimeDrift defines the time slot in which a new block can be created
	BlockTimeDrift uint64 `json:"blockTimeDrift"`

	// BlockTrackerPollInterval specifies interval
	// at which block tracker polls for blocks on a external chain
	BlockTrackerPollInterval common.Duration `json:"blockTrackerPollInterval"`

	// ProxyContractsAdmin is the address that will have the privilege to change both the proxy
	// implementation address and the admin
	ProxyContractsAdmin types.Address `json:"proxyContractsAdmin"`

	// BladeAdmin is the address that will be the owner of the NativeERC20 mintable token,
	// and StakeManager contract which manages validators
	BladeAdmin types.Address `json:"bladeAdmin"`

	// GovernanceConfig defines on chain governance configuration
	GovernanceConfig *Governance `json:"governanceConfig"`

	// StakeTokenAddr represents the stake token contract address
	StakeTokenAddr types.Address `json:"stakeTokenAddr"`
}

// LoadPolyBFTConfig loads chain config from provided path and unmarshals PolyBFTConfig
func LoadPolyBFTConfig(chainConfigFile string) (PolyBFT, error) {
	chainCfg, err := chain.ImportFromFile(chainConfigFile)
	if err != nil {
		return PolyBFT{}, err
	}

	polybftConfig, err := GetPolyBFTConfig(chainCfg.Params)
	if err != nil {
		return PolyBFT{}, err
	}

	return polybftConfig, err
}

// GetPolyBFTConfig deserializes provided chain config and returns PolyBFTConfig
func GetPolyBFTConfig(chainParams *chain.Params) (PolyBFT, error) {
	consensusConfigJSON, err := json.Marshal(chainParams.Engine[ConsensusName])
	if err != nil {
		return PolyBFT{}, err
	}

	var polyBFTConfig PolyBFT
	if err = json.Unmarshal(consensusConfigJSON, &polyBFTConfig); err != nil {
		return PolyBFT{}, err
	}

	return polyBFTConfig, nil
}

// Bridge is the external chain configuration, needed for bridging
type Bridge struct {
	// External chain bridge contracts
	ExternalGatewayAddr                  types.Address `json:"externalGatewayAddress"`
	ExternalERC20PredicateAddr           types.Address `json:"externalERC20PredicateAddress"`
	ExternalMintableERC20PredicateAddr   types.Address `json:"externalMintableERC20PredicateAddress"`
	ExternalNativeERC20Addr              types.Address `json:"nativeERC20Address"`
	ExternalERC721PredicateAddr          types.Address `json:"externalERC721PredicateAddress"`
	ExternalMintableERC721PredicateAddr  types.Address `json:"externalERC721MintablePredicateAddress"`
	ExternalERC1155PredicateAddr         types.Address `json:"externalERC1155PredicateAddress"`
	ExternalMintableERC1155PredicateAddr types.Address `json:"externalERC1155MintablePredicateAddress"`
	ExternalERC20Addr                    types.Address `json:"externalERC20Address"`
	ExternalERC721Addr                   types.Address `json:"externalERC721Address"`
	ExternalERC1155Addr                  types.Address `json:"externalERC1155Address"`
	BladeManagerAddr                     types.Address `json:"bladeManagerAddress"`
	// only populated if stake-manager-deploy command is executed, and used for e2e tests
	BLSAddress     types.Address `json:"blsAddr"`
	BN256G2Address types.Address `json:"bn256G2Addr"`

	// Blade bridge contracts
	InternalGatewayAddr                  types.Address `json:"internalGatewayAddress"`
	InternalERC20PredicateAddr           types.Address `json:"internalERC20PredicateAddress"`
	InternalERC721PredicateAddr          types.Address `json:"internalERC721PredicateAddress"`
	InternalERC1155PredicateAddr         types.Address `json:"internalERC1155PredicateAddress"`
	InternalMintableERC20PredicateAddr   types.Address `json:"internalMintableERC20PredicateAddress"`
	InternalMintableERC721PredicateAddr  types.Address `json:"internalMintableERC721PredicateAddress"`
	InternalMintableERC1155PredicateAddr types.Address `json:"internalMintableERC1155PredicateAddress"`

	// Event tracker
	JSONRPCEndpoint         string                   `json:"jsonRPCEndpoint"`
	EventTrackerStartBlocks map[types.Address]uint64 `json:"eventTrackerStartBlocks"`
}

// GetHighestInternalAddress returns the highest address among all internal bridge contracts
func (b *Bridge) GetHighestInternalAddress() types.Address {
	internalAddrs := b.getInternalContractAddrs()

	if len(internalAddrs) == 0 {
		return types.ZeroAddress
	}

	highestAddr := internalAddrs[0]

	for _, addr := range internalAddrs[1:] {
		if addr.Compare(highestAddr) > 0 {
			highestAddr = addr
		}
	}

	return highestAddr
}

// getInternalContractAddrs enumerates all the Internal bridge contract addresses
func (b *Bridge) getInternalContractAddrs() []types.Address {
	return []types.Address{
		b.InternalGatewayAddr,
		b.InternalERC20PredicateAddr,
		b.InternalERC721PredicateAddr,
		b.InternalERC1155PredicateAddr,
		b.InternalMintableERC20PredicateAddr,
		b.InternalMintableERC721PredicateAddr,
		b.InternalMintableERC1155PredicateAddr,
	}
}

func (p *PolyBFT) IsBridgeEnabled() bool {
	return len(p.Bridge) > 0
}

// Token is the configuration of native token used by edge network
type Token struct {
	Name       string `json:"name"`
	Symbol     string `json:"symbol"`
	Decimals   uint8  `json:"decimals"`
	IsMintable bool   `json:"isMintable"`
	ChainID    uint64 `json:"chainID"`
}

func ParseRawTokenConfig(rawConfig string) (*Token, error) {
	if rawConfig == "" {
		return DefaultTokenConfig, nil
	}

	params := strings.Split(rawConfig, ":")
	if len(params) < minNativeTokenParamsNumber {
		return nil, errInvalidTokenParams
	}

	// name
	name := strings.TrimSpace(params[0])
	if name == "" {
		return nil, errInvalidTokenParams
	}

	// symbol
	symbol := strings.TrimSpace(params[1])
	if symbol == "" {
		return nil, errInvalidTokenParams
	}

	// decimals
	decimals, err := strconv.ParseUint(strings.TrimSpace(params[2]), 10, 8)
	if err != nil || decimals > math.MaxUint8 {
		return nil, errInvalidTokenParams
	}

	// is mintable native token used
	isMintable, err := strconv.ParseBool(strings.TrimSpace(params[3]))
	if err != nil {
		return nil, errInvalidTokenParams
	}

	var chainID uint64

	if !isMintable {
		if len(params) == minNativeTokenParamsNumber+1 {
			chainID, err = common.ParseUint64orHex(&params[4])
			if err != nil {
				return nil, errInvalidTokenParams
			}
		} else {
			return nil, errInvalidTokenParams
		}
	}

	return &Token{
		Name:       name,
		Symbol:     symbol,
		Decimals:   uint8(decimals),
		IsMintable: isMintable,
		ChainID:    chainID,
	}, nil
}

type Rewards struct {
	// TokenAddress is the address of reward token on child chain
	TokenAddress types.Address

	// WalletAddress is the address of reward wallet on child chain
	WalletAddress types.Address

	// WalletAmount is the amount of tokens in reward wallet
	WalletAmount *big.Int
}

func (r *Rewards) MarshalJSON() ([]byte, error) {
	raw := &rewardsConfigRaw{
		TokenAddress:  r.TokenAddress,
		WalletAddress: r.WalletAddress,
		WalletAmount:  common.EncodeBigInt(r.WalletAmount),
	}

	return json.Marshal(raw)
}

func (r *Rewards) UnmarshalJSON(data []byte) error {
	var (
		raw rewardsConfigRaw
		err error
	)

	if err = json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.TokenAddress = raw.TokenAddress
	r.WalletAddress = raw.WalletAddress

	r.WalletAmount, err = common.ParseUint256orHex(raw.WalletAmount)
	if err != nil {
		return err
	}

	return nil
}

type Governance struct {
	// VotingDelay indicates number of blocks after proposal is submitted before voting starts
	VotingDelay *big.Int
	// VotingPeriod indicates number of blocks that the voting period for a proposal lasts
	VotingPeriod *big.Int
	// ProposalThreshold indicates number of vote tokens required in order for a voter to become a proposer
	ProposalThreshold *big.Int
	// ProposalQuorumPercentage is the percentage of total validator stake needed for a
	// governance proposal to be accepted
	ProposalQuorumPercentage uint64
	// ChildGovernorAddr is the address of ChildGovernor contract
	ChildGovernorAddr types.Address
	// ChildTimelockAddr is the address of ChildTimelock contract
	ChildTimelockAddr types.Address
	// NetworkParamsAddr is the address of NetworkParams contract
	NetworkParamsAddr types.Address
	// ForkParamsAddr is the address of ForkParams contract
	ForkParamsAddr types.Address
}

func (g *Governance) MarshalJSON() ([]byte, error) {
	raw := &governanceConfigRaw{
		VotingDelay:              common.EncodeBigInt(g.VotingDelay),
		VotingPeriod:             common.EncodeBigInt(g.VotingPeriod),
		ProposalThreshold:        common.EncodeBigInt(g.ProposalThreshold),
		ProposalQuorumPercentage: g.ProposalQuorumPercentage,
		ChildGovernorAddr:        g.ChildGovernorAddr,
		ChildTimelockAddr:        g.ChildTimelockAddr,
		NetworkParamsAddr:        g.NetworkParamsAddr,
		ForkParamsAddr:           g.ForkParamsAddr,
	}

	return json.Marshal(raw)
}

func (g *Governance) UnmarshalJSON(data []byte) error {
	var (
		raw governanceConfigRaw
		err error
	)

	if err = json.Unmarshal(data, &raw); err != nil {
		return err
	}

	g.VotingDelay, err = common.ParseUint256orHex(raw.VotingDelay)
	if err != nil {
		return err
	}

	g.VotingPeriod, err = common.ParseUint256orHex(raw.VotingPeriod)
	if err != nil {
		return err
	}

	g.ProposalThreshold, err = common.ParseUint256orHex(raw.ProposalThreshold)
	if err != nil {
		return err
	}

	g.ProposalQuorumPercentage = raw.ProposalQuorumPercentage
	g.ChildGovernorAddr = raw.ChildGovernorAddr
	g.ChildTimelockAddr = raw.ChildTimelockAddr
	g.NetworkParamsAddr = raw.NetworkParamsAddr
	g.ForkParamsAddr = raw.ForkParamsAddr

	return nil
}

type governanceConfigRaw struct {
	VotingDelay              *string       `json:"votingDelay"`
	VotingPeriod             *string       `json:"votingPeriod"`
	ProposalThreshold        *string       `json:"proposalThreshold"`
	ProposalQuorumPercentage uint64        `json:"proposalQuorumPercentage"`
	ChildGovernorAddr        types.Address `json:"childGovernorAddr"`
	ChildTimelockAddr        types.Address `json:"childTimelockAddr"`
	NetworkParamsAddr        types.Address `json:"networkParamsAddr"`
	ForkParamsAddr           types.Address `json:"forkParamsAddr"`
}

type rewardsConfigRaw struct {
	TokenAddress  types.Address `json:"rewardTokenAddress"`
	WalletAddress types.Address `json:"rewardWalletAddress"`
	WalletAmount  *string       `json:"rewardWalletAmount"`
}

type BurnContractInfo struct {
	BlockNumber        uint64
	Address            types.Address
	DestinationAddress types.Address
}
