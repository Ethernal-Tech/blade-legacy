package contractsapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArtifactNotEmpty(t *testing.T) {
	require.NotEmpty(t, Gateway.Bytecode)
	require.NotEmpty(t, Gateway.DeployedBytecode)
	require.NotEmpty(t, Gateway.Abi)

	require.NotEmpty(t, BridgeStorage.Bytecode)
	require.NotEmpty(t, BridgeStorage.DeployedBytecode)
	require.NotEmpty(t, BridgeStorage.Abi)

	require.NotEmpty(t, BLS.Bytecode)
	require.NotEmpty(t, BLS.DeployedBytecode)
	require.NotEmpty(t, BLS.Abi)

	require.NotEmpty(t, BLS256.Bytecode)
	require.NotEmpty(t, BLS256.DeployedBytecode)
	require.NotEmpty(t, BLS256.Abi)
}
