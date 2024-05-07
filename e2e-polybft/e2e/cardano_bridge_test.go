package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
)

func TestE2E_CardanoBridgeTest(t *testing.T) {
	const (
		dataDir = "../../e2e-bridge-data-tmp"

		bladeValidatorsNum = 4
		bladeEpochSize     = 5

		primeNetworkAddress = "http://prime_network_address"
		primeNetworkMagic   = 142
		primeOgmiosURL      = "http://testPrimeOgmiosUrl"

		vectorNetworkAddress = "http://vector_network_address"
		vectorNetworkMagic   = 242
		vectorOgmiosURL      = "http://testVectorOgmiosUrl"
	)

	os.Setenv("E2E_TESTS", "true")

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	cleanup := cardanofw.SetupAndRunApexBridge(
		t,
		ctx,
		dataDir,
		bladeValidatorsNum,
		bladeEpochSize,
		primeNetworkAddress,
		primeNetworkMagic,
		primeOgmiosURL,
		vectorNetworkAddress,
		vectorNetworkMagic,
		vectorOgmiosURL,
	)

	defer cleanup()

	time.Sleep(100 * time.Second)
}
