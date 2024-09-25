package validator

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/0xPolygon/polygon-edge/types"
)

var bigZero = big.NewInt(0)

type ValidatorSetState struct {
	BlockNumber          uint64            `json:"block"`
	EpochID              uint64            `json:"epoch"`
	UpdatedAtBlockNumber uint64            `json:"updated_at_block"`
	Validators           ValidatorStakeMap `json:"validators"`
}

func (vs ValidatorSetState) Marshal() ([]byte, error) {
	return json.Marshal(vs)
}

func (vs *ValidatorSetState) Unmarshal(b []byte) error {
	return json.Unmarshal(b, vs)
}

// ValidatorStakeMap holds ValidatorMetadata for each validator address
type ValidatorStakeMap map[types.Address]*ValidatorMetadata

// NewValidatorStakeMap returns a new instance of validatorStakeMap
func NewValidatorStakeMap(validatorSet AccountSet) ValidatorStakeMap {
	stakeMap := make(ValidatorStakeMap, len(validatorSet))

	for _, v := range validatorSet {
		stakeMap[v.Address] = v.Copy()
	}

	return stakeMap
}

// AddStake adds given amount to a validator defined by address
func (sc *ValidatorStakeMap) AddStake(address types.Address, amount *big.Int) {
	if metadata, exists := (*sc)[address]; exists {
		metadata.VotingPower.Add(metadata.VotingPower, amount)
		metadata.IsActive = metadata.VotingPower.Cmp(bigZero) > 0
	} else {
		(*sc)[address] = &ValidatorMetadata{
			VotingPower: new(big.Int).Set(amount),
			Address:     address,
			IsActive:    amount.Cmp(bigZero) > 0,
		}
	}
}

// RemoveStake removes given amount from validator defined by address
func (sc *ValidatorStakeMap) RemoveStake(address types.Address, amount *big.Int) {
	stakeData := (*sc)[address]
	stakeData.VotingPower.Sub(stakeData.VotingPower, amount)
	stakeData.IsActive = stakeData.VotingPower.Cmp(bigZero) > 0
}

// GetSorted returns validators (*ValidatorMetadata) in sorted order
func (sc ValidatorStakeMap) GetSorted(maxValidatorSetSize int) AccountSet {
	activeValidators := make(AccountSet, 0, len(sc))

	for _, v := range sc {
		if v.VotingPower.Cmp(bigZero) > 0 {
			activeValidators = append(activeValidators, v)
		}
	}

	sort.Slice(activeValidators, func(i, j int) bool {
		v1, v2 := activeValidators[i], activeValidators[j]

		switch v1.VotingPower.Cmp(v2.VotingPower) {
		case 1:
			return true
		case 0:
			return bytes.Compare(v1.Address[:], v2.Address[:]) < 0
		default:
			return false
		}
	})

	if len(activeValidators) <= maxValidatorSetSize {
		return activeValidators
	}

	return activeValidators[:maxValidatorSetSize]
}

func (sc ValidatorStakeMap) String() string {
	var sb strings.Builder

	for _, x := range sc.GetSorted(len(sc)) {
		bls := ""
		if x.BlsKey != nil {
			bls = hex.EncodeToString(x.BlsKey.Marshal())
		}

		sb.WriteString(fmt.Sprintf("%s:%s:%s:%t\n",
			x.Address, x.VotingPower, bls, x.IsActive))
	}

	return sb.String()
}
