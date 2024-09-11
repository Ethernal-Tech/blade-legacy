package insert

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	privateKeyFlag = "private-key"
	passphraseFlag = "passphrase"
)

type insertParams struct {
	privateKey string
	passphrase string
	jsonRPC    string
}

type insertResult struct {
	Address types.Address `json:"address"`
}

func (i *insertResult) GetOutput() string {
	var buffer bytes.Buffer

	vals := make([]string, 0, 2)
	vals = append(vals, fmt.Sprintf("Address|%s", i.Address.String()))

	buffer.WriteString("\n[Inserted accounts]\n")
	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
