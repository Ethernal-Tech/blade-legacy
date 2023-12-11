package premine

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	bridgeHelper "github.com/0xPolygon/polygon-edge/command/bridge/helper"
	"github.com/0xPolygon/polygon-edge/command/helper"
	validatorHelper "github.com/0xPolygon/polygon-edge/command/validator/helper"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
)

var errMandatoryRootPredicateAddr = errors.New("root erc20 predicate address not defined")

const (
	nonStakedAmountFlag    = "non-staked-amount"
	stakedAmountFlag       = "staked-amount"
	rootERC20PredicateFlag = "root-erc20-predicate"
)

type premineParams struct {
	accountDir         string
	accountConfig      string
	privateKey         string
	bladeManager       string
	rootERC20Predicate string
	nativeTokenRoot    string
	jsonRPC            string
	stakedAmount       string
	nonStakedAmount    string

	nonStakedValue *big.Int
	stakedValue    *big.Int
}

func (p *premineParams) validateFlags() (err error) {
	if p.nativeTokenRoot == "" {
		return bridgeHelper.ErrMandatoryERC20Token
	}

	if err := types.IsValidAddress(p.nativeTokenRoot); err != nil {
		return fmt.Errorf("invalid erc20 token address is provided: %w", err)
	}

	if p.rootERC20Predicate == "" {
		return errMandatoryRootPredicateAddr
	}

	if err := types.IsValidAddress(p.rootERC20Predicate); err != nil {
		return fmt.Errorf("invalid root erc20 predicate address is provided: %w", err)
	}

	if p.bladeManager == "" {
		return bridgeHelper.ErrMandatoryBladeManagerAddr
	}

	if err := types.IsValidAddress(p.bladeManager); err != nil {
		return fmt.Errorf("invalid blade manager address is provided: %w", err)
	}

	if p.nonStakedValue, err = common.ParseUint256orHex(&p.nonStakedAmount); err != nil {
		return err
	}

	if p.stakedValue, err = common.ParseUint256orHex(&p.stakedAmount); err != nil {
		return err
	}

	// validate jsonrpc address
	if _, err := helper.ParseJSONRPCAddress(p.jsonRPC); err != nil {
		return fmt.Errorf("failed to parse json rpc address. Error: %w", err)
	}

	if p.privateKey == "" {
		return validatorHelper.ValidateSecretFlags(p.accountDir, p.accountConfig)
	}

	return nil
}

type premineResult struct {
	Address string   `json:"address"`
	Amount  *big.Int `json:"amount"`
}

func (p premineResult) GetOutput() string {
	var buffer bytes.Buffer

	buffer.WriteString("\n[NATIVE ROOT TOKEN PREMINE]\n")

	vals := make([]string, 0, 2)
	vals = append(vals, fmt.Sprintf("Address|%s", p.Address))
	vals = append(vals, fmt.Sprintf("Amount Premined|%d", p.Amount))

	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
