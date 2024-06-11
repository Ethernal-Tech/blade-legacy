package create

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	PassphraseFlag = "passphrase"
	ConfigDirFlag  = "config-dir"
)

type createParams struct {
	passphrase string
	configDir  string
}

type createResult struct {
	Address types.Address `json:"address"`
}

func (i *createResult) GetOutput() string {
	var buffer bytes.Buffer

	vals := make([]string, 0, 1)
	vals = append(vals, fmt.Sprintf("Address|%s", i.Address.String()))

	buffer.WriteString("\n[Import accounts]\n")
	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
