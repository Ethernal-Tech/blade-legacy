package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/stretchr/testify/require"
)

func TestE2E_CardanoBridgeTest(t *testing.T) {
	const (
		dataDir = "../../e2e-bridge-data-tmp"

		validatorsNum = 4
		epochSize     = 5

		NetworkMagicPrime  = 142
		NetworkMagicVector = 242
	)

	cleanup := func() {
		os.RemoveAll(dataDir)
	}

	os.Setenv("E2E_TESTS", "true")

	cleanup()
	// defer cleanup()

	cb := cardanofw.NewTestCardanoBridge(dataDir, validatorsNum)

	require.NoError(t, cb.CardanoCreateWalletsAndAddresses(NetworkMagicPrime, NetworkMagicVector))

	//nolint:godox
	// TODO: setup cb.PrimeMultisigAddr and rest to cardano chains
	// send initial utxos and such

	cb.StartValidators(t, epochSize)
	defer cb.StopValidators()

	cb.WaitForValidatorsReady(t)
	/*
		// need params for it to work properly
		require.NoError(t, cb.RegisterChains(
			big.NewInt(1000),
			"http://testPrimeBFUrl",
			big.NewInt(1000),
			"http://testVectorBFUrl",
		))
	*/

	// need params for it to work properly
	require.NoError(t, cb.GenerateConfigs(
		"http://prime_network_address",
		NetworkMagicPrime,
		"http://testPrimeBFUrl",
		"http://vector_network_address",
		NetworkMagicVector,
		"http://testVectorBFUrl",
		40000,
		"test_api_key",
	))

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	require.NoError(t, cb.StartValidatorComponents(ctx))
	require.NoError(t, cb.StartRelayer(ctx))

	time.Sleep(100 * time.Second)
}
