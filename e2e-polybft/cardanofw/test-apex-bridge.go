package cardanofw

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func SetupAndRunApexBridge(
	t *testing.T,
	ctx context.Context,
	dataDir string,
	bladeValidatorsNum int,
	bladeEpochSize int,
	primeNetworkAddress string,
	primeNetworkMagic int,
	primeOgmiosURL string,
	vectorNetworkAddress string,
	vectorNetworkMagic int,
	vectorOgmiosURL string,
) (*TestCardanoBridge, func()) {
	t.Helper()

	cleanupDataDir := func() {
		os.RemoveAll(dataDir)
	}

	cleanupDataDir()

	cb := NewTestCardanoBridge(dataDir, bladeValidatorsNum)

	require.NoError(t, cb.CardanoCreateWalletsAndAddresses(primeNetworkMagic, vectorNetworkMagic))

	//nolint:godox
	// TODO: setup cb.PrimeMultisigAddr and rest to cardano chains
	// send initial utxos and such

	cb.StartValidators(t, bladeEpochSize)

	cb.WaitForValidatorsReady(t)

	/*
		// need params for it to work properly
		primeTokenSupply := big.NewInt(1000)
		vectorTokenSupply := big.NewInt(1000)
			require.NoError(t, cb.RegisterChains(
				primeTokenSupply,
				primeOgmiosURL,
				vectorTokenSupply,
				vectorOgmiosURL,
			))
	*/

	// need params for it to work properly
	require.NoError(t, cb.GenerateConfigs(
		primeNetworkAddress,
		primeNetworkMagic,
		primeOgmiosURL,
		vectorNetworkAddress,
		vectorNetworkMagic,
		vectorOgmiosURL,
		40000,
		"test_api_key",
	))

	require.NoError(t, cb.StartValidatorComponents(ctx))
	require.NoError(t, cb.StartRelayer(ctx))

	return cb, func() {
		// cleanupDataDir()
		cb.StopValidators()
	}
}
