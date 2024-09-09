package deploy

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
)

type deployContractResult struct {
	Name    string        `json:"name"`
	Address types.Address `json:"address"`
	Hash    types.Hash    `json:"hash"`
	GasUsed uint64        `json:"gasUsed"`
	isProxy bool          `json:"-"`
}

func newDeployContractsResult(name string,
	isProxy bool,
	address types.Address,
	hash ethgo.Hash, gasUsed uint64) *deployContractResult {
	return &deployContractResult{
		Name:    name,
		Address: address,
		Hash:    types.BytesToHash(hash.Bytes()),
		GasUsed: gasUsed,
		isProxy: isProxy,
	}
}

func (r deployContractResult) GetOutput() string {
	var buffer bytes.Buffer

	buffer.WriteString("\n[BRIDGE - DEPLOY CONTRACT]\n")

	vals := make([]string, 0, 4)
	vals = append(vals, fmt.Sprintf("Name|%s", r.Name))
	vals = append(vals, fmt.Sprintf("Contract (address)|%s", r.Address))
	vals = append(vals, fmt.Sprintf("Transaction (hash)|%s", r.Hash))
	vals = append(vals, fmt.Sprintf("Transaction (gas used)|%d", r.GasUsed))

	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
