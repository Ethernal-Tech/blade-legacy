package registration

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/command/helper"
	sidechainHelper "github.com/0xPolygon/polygon-edge/command/validator/helper"
)

type registerParams struct {
	accountDir    string
	accountConfig string
	jsonRPC       string
	amount        string

	amountValue *big.Int
}

func (rp *registerParams) validateFlags() (err error) {
	if rp.amountValue, err = helper.ParseAmountAllowZero(rp.amount); err != nil {
		return err
	}

	// validate jsonrpc address
	_, err = helper.ParseJSONRPCAddress(rp.jsonRPC)
	if err != nil {
		return fmt.Errorf("failed to parse json rpc address. Error: %w", err)
	}

	return sidechainHelper.ValidateSecretFlags(rp.accountDir, rp.accountConfig)
}

type registerResult struct {
	validatorAddress string
	koskSignature    string
	amount           *big.Int
}

func (rr registerResult) GetOutput() string {
	var buffer bytes.Buffer

	buffer.WriteString("\n[VALIDATOR REGISTRATION]\n")

	vals := make([]string, 0, 2)
	vals = append(vals, fmt.Sprintf("Validator Address|%s", rr.validatorAddress))
	vals = append(vals, fmt.Sprintf("KOSK Signature|%s", rr.koskSignature))
	vals = append(vals, fmt.Sprintf("Amount|%s", rr.amount))
	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
