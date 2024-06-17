package create

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/types"
)

const passphraseFlag = "passphrase"

type createParams struct {
	passphrase string
	jsonRPC    string
}

type createResult struct {
	Address types.Address `json:"address"`
}

func (i *createResult) GetOutput() string {
	var buffer bytes.Buffer

	vals := make([]string, 0, 2)
	vals = append(vals, fmt.Sprintf("Address|%s", i.Address.String()))

	buffer.WriteString("\n[Created accounts]\n")
	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
