package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

var (
	InvalidState = "InvalidRequest"
)

// Download Cardano executables from https://github.com/IntersectMBO/cardano-node/releases/tag/8.7.3 and unpack tar.gz file
// Add directory where unpacked files are located to the $PATH (in example bellow `~/Apps/cardano`)
// eq add line `export PATH=$PATH:~/Apps/cardano` to  `~/.bashrc`
func TestE2E_CardanoTwoClustersBasic(t *testing.T) {
	const (
		cardanoChainsCnt = 2
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	clusters, cleanupFunc := cardanofw.SetupAndRunApexCardanoChains(
		t,
		ctx,
		cardanoChainsCnt,
	)

	for i := 0; i < cardanoChainsCnt; i++ {
		require.NotNil(t, clusters[i])
	}

	defer cleanupFunc()

	t.Run("simple send", func(t *testing.T) {
		const (
			sendAmount = uint64(1000000)
		)

		var (
			txProviders = make([]wallet.ITxProvider, cardanoChainsCnt)
			receivers   = make([]string, cardanoChainsCnt)
		)

		for i := 0; i < cardanoChainsCnt; i++ {
			require.NotNil(t, clusters[i])

			txProviders[i] = wallet.NewTxProviderOgmios(clusters[i].OgmiosURL())
			newWalletKeys, err := wallet.NewStakeWalletManager().Create(path.Join(clusters[i].Config.Dir("keys")), true)

			require.NoError(t, err)

			receiver, _, err := wallet.GetWalletAddress(newWalletKeys, uint(clusters[i].Config.NetworkMagic))
			require.NoError(t, err)

			receivers[i] = receiver

			ctx, cncl := context.WithCancel(context.Background())
			defer cncl()

			genesisWallet, err := cardanofw.GetGenesisWalletFromCluster(clusters[i].Config.TmpDir, 1)
			require.NoError(t, err)

			_, err = cardanofw.SendTx(ctx, txProviders[i], genesisWallet,
				sendAmount, receivers[i], clusters[i].Config.NetworkMagic, []byte{})
			require.NoError(t, err)
		}

		for i := 0; i < cardanoChainsCnt; i++ {
			err := wallet.WaitForAmount(context.Background(), txProviders[i], receivers[i], func(val *big.Int) bool {
				return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
			}, 60, time.Second*2)
			require.NoError(t, err)
		}
	})
}

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

		if prevStatus != currentStatus {
			fmt.Printf("currentStatus = %s\n", currentStatus)
		}

		if prevStatus == "FailedToExecuteOnDestination" && currentStatus == "IncludedInBatch" {
			wentFromFailedOnDestinationToIncludedInBatch = true

			break for_loop
		}
	}

	fmt.Printf("wentFromFailedOnDestinationToIncludedInBatch = %v\n", wentFromFailedOnDestinationToIncludedInBatch)

	require.True(t, wentFromFailedOnDestinationToIncludedInBatch)
}

func TestE2E_InvalidScenarios(t *testing.T) {
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

	sendAmount := uint64(50_000_000)
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

	t.Run("Submitter not enough funds", func(t *testing.T) {
		receivers := make(map[string]uint64, 2)
		sendAmount = uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers[user.VectorAddress] = sendAmount * 10 // 10Ada
		receivers[cb.VectorMultisigFeeAddr] = feeAmount

		bridgingRequestMetadata, err := cardanofw.CreateMetaData(user.PrimeAddress, receivers)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), cb.PrimeMultisigAddr,
			primeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := cb.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

		state, err := WaitForRequestState(InvalidState, ctx, "prime", txHash, requestURL, apiKey, 300)
		require.NoError(t, err)
		require.Equal(t, state, InvalidState)
	})

	t.Run("Multiple submiters don't have enough funds", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			receivers := make(map[string]uint64, 2)
			sendAmount = uint64(1_000_000)
			feeAmount := uint64(1_100_000)

			receivers[user.VectorAddress] = sendAmount * 10 // 10Ada
			receivers[cb.VectorMultisigFeeAddr] = feeAmount

			bridgingRequestMetadata, err := cardanofw.CreateMetaData(user.PrimeAddress, receivers)
			require.NoError(t, err)

			txHash, err := cardanofw.SendTx(
				ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), cb.PrimeMultisigAddr,
				primeCluster.Config.NetworkMagic, bridgingRequestMetadata)
			require.NoError(t, err)

			apiURL, err := cb.GetBridgingAPI()
			require.NoError(t, err)

			requestURL := fmt.Sprintf(
				"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

			state, err := WaitForRequestState(InvalidState, ctx, "prime", txHash, requestURL, apiKey, 300)
			require.NoError(t, err)
			require.Equal(t, state, InvalidState)
		}
	})

	t.Run("Multiple submiters don't have enough funds parallel", func(t *testing.T) {
		instances := 5
		walletKeys := make([]wallet.IWallet, instances)
		txHashes := make([]string, instances)

		for i := 0; i < instances; i++ {
			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(primeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walledAddress, _, err := wallet.GetWalletAddress(walletKeys[i], uint(primeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			sendAmount = uint64(5_000_000)
			_, err = cardanofw.SendTx(ctx, txProviderPrime, primeGenesisWallet,
				sendAmount, walledAddress, primeCluster.Config.NetworkMagic, []byte{})
			require.NoError(t, err)
			time.Sleep(time.Second * 5)
		}

		sendAmount = uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			receivers := make(map[string]uint64, 2)
			receivers[user.VectorAddress] = sendAmount * 10 // 10Ada
			receivers[cb.VectorMultisigFeeAddr] = feeAmount

			bridgingRequestMetadata, err := cardanofw.CreateMetaData(user.PrimeAddress, receivers)
			require.NoError(t, err)

			wg.Add(1)
			idx := i
			go func() {
				txHashes[idx], err = cardanofw.SendTx(
					ctx, txProviderPrime, walletKeys[idx], (sendAmount + feeAmount), cb.PrimeMultisigAddr,
					primeCluster.Config.NetworkMagic, bridgingRequestMetadata)
				require.NoError(t, err)
				wg.Done()
			}()
		}

		wg.Wait()
		for i := 0; i < instances; i++ {
			apiURL, err := cb.GetBridgingAPI()
			require.NoError(t, err)

			requestURL := fmt.Sprintf(
				"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHashes[i])

			state, err := WaitForRequestState(InvalidState, ctx, "prime", txHashes[i], requestURL, apiKey, 300)
			require.NoError(t, err)
			require.Equal(t, state, InvalidState)
		}
	})

	t.Run("Submited invalid metadata", func(t *testing.T) {
		sendAmount = uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers := make(map[string]uint64, 2)
		receivers[user.VectorAddress] = sendAmount
		receivers[cb.VectorMultisigFeeAddr] = feeAmount

		bridgingRequestMetadata, err := cardanofw.CreateMetaData(user.PrimeAddress, receivers)
		require.NoError(t, err)

		// Send only half bytes of metadata making it invalid
		bridgingRequestMetadata = bridgingRequestMetadata[0 : len(bridgingRequestMetadata)/2]

		_, err = cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), cb.PrimeMultisigAddr,
			primeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.Error(t, err)
	})
}

func TestE2E_ValidScenarios(t *testing.T) {
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
	txProviderVector := wallet.NewTxProviderOgmios(vectorCluster.OgmiosURL())

	sendAmount := uint64(50_000_000)
	// Fund prime address
	primeGenesisWallet, err := cardanofw.GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	user.SendToUser(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, true)

	fmt.Printf("Prime user address funded\n")

	// Fund vector address
	vectorGenesisWallet, err := cardanofw.GetGenesisWalletFromCluster(vectorCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	user.SendToUser(t, ctx, txProviderVector, vectorGenesisWallet, sendAmount, false)

	fmt.Printf("Vector user address funded\n")

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

	t.Run("From prime to vector one by one", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			sendAmount = uint64(1_000_000)

			user.BridgeAmount(t, ctx, txProviderPrime, cb.PrimeMultisigAddr,
				cb.VectorMultisigFeeAddr, sendAmount, true)

			require.NoError(t, err)
			fmt.Printf("Tx %v confirmed", i+1)
		}
	})

	t.Run("From prime to vector parallel", func(t *testing.T) {
		instances := 5
		walletKeys := make([]wallet.IWallet, instances)

		for i := 0; i < instances; i++ {
			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(primeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walledAddress, _, err := wallet.GetWalletAddress(walletKeys[i], uint(primeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			sendAmount := uint64(5_000_000)
			user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, walledAddress, true)
		}

		sendAmount = uint64(1_000_000)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)
			go func() {
				user.BridgeAmount(t, ctx, txProviderPrime, cb.PrimeMultisigAddr, cb.VectorMultisigFeeAddr, sendAmount, true)
				wg.Done()
			}()
		}

		wg.Wait()

		expectedTotal := 50_000_000 + uint64(instances)*sendAmount
		err = wallet.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedTotal)) == 0
		}, 100, time.Minute*5)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed", instances)
	})

	t.Run("From vector to prime one by one", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			sendAmount = uint64(1_000_000)

			user.BridgeAmount(t, ctx, txProviderPrime, cb.PrimeMultisigAddr,
				cb.VectorMultisigFeeAddr, sendAmount, true)

			fmt.Printf("Tx %v confirmed", i+1)
		}
	})

	t.Run("From vector to prime parallel", func(t *testing.T) {
		instances := 5
		walletKeys := make([]wallet.IWallet, instances)

		for i := 0; i < instances; i++ {
			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(vectorCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walledAddress, _, err := wallet.GetWalletAddress(walletKeys[i], uint(vectorCluster.Config.NetworkMagic))
			require.NoError(t, err)

			sendAmount = uint64(5_000_000)
			user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, walledAddress, true)
		}

		sendAmount = uint64(1_000_000)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)
			go func() {
				user.BridgeAmount(t, ctx, txProviderVector, cb.VectorMultisigAddr, cb.PrimeMultisigFeeAddr, sendAmount, false)
				wg.Done()
			}()
		}

		wg.Wait()

		expectedTotal := 50_000_000 + uint64(instances)*sendAmount
		err = wallet.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedTotal)) == 0
		}, 100, time.Minute*5)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed", instances)
	})
}
func WaitForRequestState(expectedState string, ctx context.Context, chainId string, txHash string,
	requestURL string, apiKey string, timeout uint) (string, error) {

	var previousState, currentState string

	for {
		previousState = currentState
		currentState, err := GetBridgingRequestState(ctx, chainId, txHash, requestURL, apiKey, timeout)
		if err != nil {
			return "", err
		}

		if strings.Compare(previousState, currentState) != 0 {
			fmt.Println(currentState)
		}

		if strings.Compare(currentState, expectedState) == 0 {
			return currentState, nil
		}
	}
}

func GetBridgingRequestState(ctx context.Context, chainId string, txHash string,
	requestURL string, apiKey string, timeout uint) (string, error) {

	timeoutTimer := time.NewTimer(time.Second * time.Duration(timeout))
	defer timeoutTimer.Stop()

	for {
		select {
		case <-timeoutTimer.C:
			fmt.Printf("Timeout\n")

			return "", errors.New("Timeout")
		case <-ctx.Done():
			fmt.Printf("Done\n")

			return "", errors.New("Done")
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

		return responseModel.Status, nil
	}
}

type BridgingRequestStateResponse struct {
	SourceChainID      string `json:"sourceChainId"`
	SourceTxHash       string `json:"sourceTxHash"`
	DestinationChainID string `json:"destinationChainId"`
	Status             string `json:"status"`
	DestinationTxHash  string `json:"destinationTxHash"`
}
