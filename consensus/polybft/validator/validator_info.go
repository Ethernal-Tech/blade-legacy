package validator

import (
	"math/big"

	"github.com/0xPolygon/polygon-edge/types"
)

// ValidatorInfo is data transfer object which holds validator information,
// provided by smart contract
type ValidatorInfo struct {
	Stake               *big.Int      `json:"stake"`
	WithdrawableRewards *big.Int      `json:"withdrawableRewards"`
	Address             types.Address `json:"address"`
	IsActive            bool          `json:"isActive"`
	IsWhitelisted       bool          `json:"isWhitelisted"`
}
