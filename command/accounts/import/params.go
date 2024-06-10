package insert

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	PrivateKeyFlag = "private-key"
	KeyDirFlag     = "key-dir"
	PassphraseFlag = "passphrase"
)

type insertParams struct {
	privateKey string
	keyDir     string
	passphrase string
}

type insertResult struct {
	Address types.Address `json:"address"`
}

func (i *insertResult) GetOutput() string {
	var buffer bytes.Buffer

	vals := make([]string, 0, 1)
	vals = append(vals, fmt.Sprintf("Address|%s", i.Address.String()))

	buffer.WriteString("\n[Import accounts]\n")
	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
