package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"log"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/common"
)

const (
	extension = ".sol"
)

func main() {
	_, filename, _, _ := runtime.Caller(0) //nolint: dogsled
	currentPath := path.Dir(filename)
	scpath := path.Join(currentPath, "../../../../apex-bridge-smartcontracts/artifacts/contracts/")

	str := `// This is auto-generated file. DO NOT EDIT.
package contractsapi

`

	apexContracts := []struct {
		Path string
		Name string
	}{
		{
			"BridgeContract.sol",
			"BridgeContract",
		},
		{
			"ClaimsHelper.sol",
			"ClaimsHelper",
		},
		{
			"ClaimsManager.sol",
			"ClaimsManager",
		},
		{
			"SignedBatchManager.sol",
			"SignedBatchManager",
		},
		{
			"SlotsManager.sol",
			"SlotsManager",
		},
		{
			"UTXOsManager.sol",
			"UTXOsManager",
		},
		{
			"ValidatorsContract.sol",
			"ValidatorsContract",
		},
	}

	for _, v := range apexContracts {
		artifactBytes, err := contracts.ReadRawArtifact(scpath, v.Path, getContractName(v.Path))
		if err != nil {
			log.Fatal(err)
		}

		dst := &bytes.Buffer{}
		if err = json.Compact(dst, artifactBytes); err != nil {
			log.Fatal(err)
		}

		str += fmt.Sprintf("var %sArtifact string = `%s`\n", v.Name, dst.String())
	}

	// Proxy contract
	scpath = path.Join(currentPath, "../../../../openzeppelin-contracts/artifacts/contracts/proxy/ERC1967")
	artifactBytes, err := contracts.ReadRawArtifact(scpath, "ERC1967Proxy.sol", getContractName("ERC1967Proxy"))
	if err != nil {
		log.Fatal(err)
	}

	dst := &bytes.Buffer{}
	if err = json.Compact(dst, artifactBytes); err != nil {
		log.Fatal(err)
	}

	str += fmt.Sprintf("var %sArtifact string = `%s`\n", "ApexProxy", dst.String())

	output, err := format.Source([]byte(str))
	if err != nil {
		fmt.Println(str)
		log.Fatal(err)
	}

	if err = common.SaveFileSafe(currentPath+"/../apex_sc_data.go", output, 0600); err != nil {
		log.Fatal(err)
	}

}

// getContractName extracts smart contract name from provided path
func getContractName(path string) string {
	pathSegments := strings.Split(path, string([]rune{os.PathSeparator}))
	nameSegment := pathSegments[len(pathSegments)-1]

	return strings.Split(nameSegment, extension)[0]
}
