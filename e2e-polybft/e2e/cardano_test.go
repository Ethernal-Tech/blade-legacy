package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

// Download Cardano executables from https://github.com/IntersectMBO/cardano-node/releases/tag/8.7.3 and unpack tar.gz file
// Add directory where unpacked files are located to the $PATH (in example bellow `~/Apps/cardano`)
// eq add line `export PATH=$PATH:~/Apps/cardano` to  `~/.bashrc`
func TestE2E_ApexBridge(t *testing.T) {
	const (
		cardanoChainsCnt   = 2
		bladeValidatorsNum = 4
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	clusters, _ := cardanofw.SetupAndRunApexCardanoChains(
		t,
		ctx,
		cardanoChainsCnt,
	)

	primeCluster := clusters[0]
	require.NotNil(t, primeCluster)

	vectorCluster := clusters[1]
	require.NotNil(t, vectorCluster)

	// defer cleanupCardanoChainsFunc()

	user := cardanofw.NewTestApexUser(
		t, uint(primeCluster.Config.NetworkMagic), uint(vectorCluster.Config.NetworkMagic))
	defer user.Dispose()

	txProviderPrime := wallet.NewTxProviderOgmios(primeCluster.OgmiosURL())
	txProviderVector := wallet.NewTxProviderOgmios(vectorCluster.OgmiosURL())

	// Fund prime address
	primeGenesisWallet, err := cardanofw.GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	sendAmount := uint64(5_000_000)
	user.SendToUser(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, true)

	fmt.Printf("Prime user address funded\n")

	cb, _ := cardanofw.SetupAndRunApexBridge(t,
		ctx,
		// path.Join(path.Dir(primeCluster.Config.TmpDir), "bridge"),
		"../../e2e-bridge-data-tmp",
		bladeValidatorsNum,
		primeCluster,
		vectorCluster,
	)
	// defer cleanupApexBridgeFunc()

	fmt.Printf("Apex bridge setup done\n")

	// Initiate bridging PRIME -> VECTOR
	sendAmount = uint64(1_000_000)

	user.BridgeAmount(t, ctx, txProviderPrime, cb.PrimeMultisigAddr,
		cb.VectorMultisigFeeAddr, sendAmount, true)

	time.Sleep(time.Second * 60)
	err = wallet.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 200, time.Second*20)
	require.NoError(t, err)
}

func TestE2E_ApexBridge_BatchRecreated(t *testing.T) {
	const (
		cardanoChainsCnt   = 2
		bladeValidatorsNum = 4
		apiKey             = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	clusters, _ := cardanofw.SetupAndRunApexCardanoChains(
		t,
		ctx,
		cardanoChainsCnt,
	)

	primeCluster := clusters[0]
	require.NotNil(t, primeCluster)

	vectorCluster := clusters[1]
	require.NotNil(t, vectorCluster)

	// defer cleanupCardanoChainsFunc()

	user := cardanofw.NewTestApexUser(
		t, uint(primeCluster.Config.NetworkMagic), uint(vectorCluster.Config.NetworkMagic))
	defer user.Dispose()

	txProviderPrime := wallet.NewTxProviderOgmios(primeCluster.OgmiosURL())

	// Fund prime address
	primeGenesisWallet, err := cardanofw.GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	sendAmount := uint64(5_000_000)
	user.SendToUser(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, true)

	fmt.Printf("Prime user address funded\n")

	cb, _ := cardanofw.SetupAndRunApexBridge(t,
		ctx,
		// path.Join(path.Dir(primeCluster.Config.TmpDir), "bridge"),
		"../../e2e-bridge-data-tmp",
		bladeValidatorsNum,
		primeCluster,
		vectorCluster,
		cardanofw.WithTTLInc(1),
		cardanofw.WithAPIKey(apiKey),
	)
	// defer cleanupApexBridgeFunc()

	fmt.Printf("Apex bridge setup done\n")

	// Initiate bridging PRIME -> VECTOR
	sendAmount = uint64(1_000_000)

	txHash := user.BridgeAmount(t, ctx, txProviderPrime, cb.PrimeMultisigAddr,
		cb.VectorMultisigFeeAddr, sendAmount, true)

	timeoutTimer := time.NewTimer(time.Second * 300)
	defer timeoutTimer.Stop()

	wentFromFailedOnDestinationToIncludedInBatch := false

	var (
		prevStatus    string
		currentStatus string
	)

	apiURL, err := cb.GetBridgingAPI()
	require.NoError(t, err)

	requestURL := fmt.Sprintf(
		"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

	fmt.Printf("Bridging request txHash = %s\n", txHash)

for_loop:
	for {
		select {
		case <-timeoutTimer.C:
			fmt.Printf("Timeout\n")

			break for_loop
		case <-ctx.Done():
			fmt.Printf("Done\n")

			break for_loop
		case <-time.After(time.Millisecond * 500):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			continue
		}

		req.Header.Set("X-API-KEY", apiKey)
		resp, err := http.DefaultClient.Do(req)
		if resp == nil || err != nil || resp.StatusCode != http.StatusOK {
			continue
		}

		resBody, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var responseModel BridgingRequestStateResponse

		err = json.Unmarshal(resBody, &responseModel)
		if err != nil {
			continue
		}

		prevStatus = currentStatus
		currentStatus = responseModel.Status

		fmt.Printf("currentStatus = %s\n", currentStatus)

		if prevStatus == "FailedToExecuteOnDestination" && currentStatus == "IncludedInBatch" {
			wentFromFailedOnDestinationToIncludedInBatch = true

			break for_loop
		}
	}

	fmt.Printf("wentFromFailedOnDestinationToIncludedInBatch = %v\n", wentFromFailedOnDestinationToIncludedInBatch)

	require.True(t, wentFromFailedOnDestinationToIncludedInBatch)
}

type BridgingRequestStateResponse struct {
	SourceChainID      string `json:"sourceChainId"`
	SourceTxHash       string `json:"sourceTxHash"`
	DestinationChainID string `json:"destinationChainId"`
	Status             string `json:"status"`
	DestinationTxHash  string `json:"destinationTxHash"`
}
