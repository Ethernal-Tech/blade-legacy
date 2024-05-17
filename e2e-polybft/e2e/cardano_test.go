package e2e

import (
	"context"
	"fmt"
	"math/big"
	"path"
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

		currentState, err := cardanofw.GetBridgingRequestState(ctx, requestURL, apiKey)
		if err != nil || currentState == nil {
			continue
		}

		prevStatus = currentStatus
		currentStatus = currentState.Status

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

		bridgingRequestMetadata, err := cardanofw.CreateMetaData(
			user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), cb.PrimeMultisigAddr,
			primeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := cb.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

		state, err := cardanofw.WaitForRequestState(InvalidState, ctx, requestURL, apiKey, 300)
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

			bridgingRequestMetadata, err := cardanofw.CreateMetaData(
				user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
			require.NoError(t, err)

			txHash, err := cardanofw.SendTx(
				ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), cb.PrimeMultisigAddr,
				primeCluster.Config.NetworkMagic, bridgingRequestMetadata)
			require.NoError(t, err)

			apiURL, err := cb.GetBridgingAPI()
			require.NoError(t, err)

			requestURL := fmt.Sprintf(
				"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

			state, err := cardanofw.WaitForRequestState(InvalidState, ctx, requestURL, apiKey, 300)
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
			idx := i
			receivers := make(map[string]uint64, 2)
			receivers[user.VectorAddress] = sendAmount * 10 // 10Ada
			receivers[cb.VectorMultisigFeeAddr] = feeAmount

			bridgingRequestMetadata, err := cardanofw.CreateMetaData(
				user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
			require.NoError(t, err)

			wg.Add(1)

			go func() {
				defer wg.Done()

				txHashes[idx], err = cardanofw.SendTx(
					ctx, txProviderPrime, walletKeys[idx], (sendAmount + feeAmount), cb.PrimeMultisigAddr,
					primeCluster.Config.NetworkMagic, bridgingRequestMetadata)
				require.NoError(t, err)
			}()
		}

		wg.Wait()

		for i := 0; i < instances; i++ {
			apiURL, err := cb.GetBridgingAPI()
			require.NoError(t, err)

			requestURL := fmt.Sprintf(
				"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHashes[i])

			state, err := cardanofw.WaitForRequestState(InvalidState, ctx, requestURL, apiKey, 300)
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

		bridgingRequestMetadata, err := cardanofw.CreateMetaData(
			user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
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
		cardanofw.WithAPIKey(apiKey),
	)
	// defer cleanupApexBridgeFunc()

	fmt.Printf("Apex bridge setup done\n")

	primeGenesisAddress, _, err := wallet.GetWalletAddress(primeGenesisWallet, uint(primeCluster.Config.NetworkMagic))
	require.NoError(t, err)

	vectorGenesisAddress, _, err := wallet.GetWalletAddress(vectorGenesisWallet, uint(vectorCluster.Config.NetworkMagic))
	require.NoError(t, err)

	fmt.Println("prime genesis addr: ", primeGenesisAddress)
	fmt.Println("vector genesis addr: ", vectorGenesisAddress)
	fmt.Println("prime user addr: ", user.PrimeAddress)
	fmt.Println("vector user addr: ", user.VectorAddress)
	fmt.Println("prime multisig addr: ", cb.PrimeMultisigAddr)
	fmt.Println("prime fee addr: ", cb.PrimeMultisigFeeAddr)
	fmt.Println("vector multisig addr: ", cb.VectorMultisigAddr)
	fmt.Println("vector fee addr: ", cb.VectorMultisigFeeAddr)

	t.Run("From prime to vector one by one - wait for other side", func(t *testing.T) {
		instances := 5
		for i := 0; i < instances; i++ {
			sendAmount = uint64(1_000_000)
			prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
			require.NoError(t, err)

			fmt.Printf("%v - prevAmount %v\n", i+1, prevAmount)

			txHash := user.BridgeAmount(t, ctx, txProviderPrime, cb.PrimeMultisigAddr,
				cb.VectorMultisigFeeAddr, sendAmount, true)

			fmt.Printf("%v - Tx sent. hash: %s\n", i+1, txHash)

			expectedAmount := prevAmount.Uint64() + sendAmount
			fmt.Printf("%v - expectedAmount %v\n", i+1, expectedAmount)

			err = wallet.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
				return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
			}, 20, time.Second*10)
			require.NoError(t, err)
		}
	})

	t.Run("From prime to vector one by one", func(t *testing.T) {
		instances := 5
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			sendAmount = uint64(1_000_000)

			txHash := user.BridgeAmount(t, ctx, txProviderPrime, cb.PrimeMultisigAddr,
				cb.VectorMultisigFeeAddr, sendAmount, true)

			fmt.Printf("Tx %v sent. hash: %s\n", i+1, txHash)
		}

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = wallet.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 20, time.Second*10)
		require.NoError(t, err)
	})

	//nolint:dupl
	t.Run("From prime to vector parallel", func(t *testing.T) {
		instances := 5
		walletKeys := make([]wallet.IWallet, instances)
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

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

			go func(idx int) {
				defer wg.Done()

				txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(primeCluster.Config.NetworkMagic),
					cb.PrimeMultisigAddr, cb.VectorMultisigFeeAddr, walletKeys[idx], user.VectorAddress, sendAmount,
					cardanofw.GetDestinationChainID(true))
				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = wallet.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", instances)

		newAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("prevAmount: %v. newAmount: %v\n", prevAmount, newAmount)
	})

	t.Run("From vector to prime one by one", func(t *testing.T) {
		instances := 5
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			sendAmount = uint64(1_000_000)

			txHash := user.BridgeAmount(t, ctx, txProviderVector, cb.VectorMultisigAddr,
				cb.PrimeMultisigFeeAddr, sendAmount, false)

			fmt.Printf("Tx %v sent. hash: %s\n", i+1, txHash)
		}

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = wallet.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 20, time.Second*10)
		require.NoError(t, err)
	})

	//nolint:dupl
	t.Run("From vector to prime parallel", func(t *testing.T) {
		instances := 5
		walletKeys := make([]wallet.IWallet, instances)
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(vectorCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walledAddress, _, err := wallet.GetWalletAddress(walletKeys[i], uint(vectorCluster.Config.NetworkMagic))
			require.NoError(t, err)

			sendAmount := uint64(5_000_000)
			user.SendToAddress(t, ctx, txProviderVector, vectorGenesisWallet, sendAmount, walledAddress, true)
		}

		sendAmount = uint64(1_000_000)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderVector, uint(vectorCluster.Config.NetworkMagic),
					cb.VectorMultisigAddr, cb.PrimeMultisigFeeAddr, walletKeys[idx], user.PrimeAddress, sendAmount,
					cardanofw.GetDestinationChainID(false))
				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = wallet.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", instances)

		newAmount, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		fmt.Printf("prevAmount: %v. newAmount: %v\n", prevAmount, newAmount)
	})

	//nolint:dupl
	t.Run("From prime to vector sequential and parallel", func(t *testing.T) {
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		sequentialInstances := 5
		parallelInstances := 6

		var wg sync.WaitGroup

		for j := 0; j < sequentialInstances; j++ {
			walletKeys := make([]wallet.IWallet, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(primeCluster.Config.Dir("keys")), true)
				require.NoError(t, err)

				walledAddress, _, err := wallet.GetWalletAddress(walletKeys[i], uint(primeCluster.Config.NetworkMagic))
				require.NoError(t, err)

				sendAmount := uint64(5_000_000)
				user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, walledAddress, true)
			}

			fmt.Printf("run: %v. Funded %v wallets \n", j+1, parallelInstances)

			sendAmount = uint64(1_000_000)

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(primeCluster.Config.NetworkMagic),
						cb.PrimeMultisigAddr, cb.VectorMultisigFeeAddr, walletKeys[idx], user.VectorAddress, sendAmount,
						cardanofw.GetDestinationChainID(true))
					fmt.Printf("run: %v. Tx %v sent. hash: %s\n", run, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()
		}

		fmt.Printf("Waiting for %v TXs\n", sequentialInstances*parallelInstances)

		expectedAmount := prevAmount.Uint64() + uint64(sequentialInstances)*uint64(parallelInstances)*sendAmount
		err = wallet.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", sequentialInstances*parallelInstances)

		newAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("prevAmount: %v. newAmount: %v\n", prevAmount, newAmount)
	})
}
