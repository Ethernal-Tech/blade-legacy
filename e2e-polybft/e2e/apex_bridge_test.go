package e2e

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

// Download Cardano executables from https://github.com/IntersectMBO/cardano-node/releases/tag/8.7.3 and unpack tar.gz file
// Add directory where unpacked files are located to the $PATH (in example bellow `~/Apps/cardano`)
// eq add line `export PATH=$PATH:~/Apps/cardano` to  `~/.bashrc`
// cd e2e-polybft/e2e
// ONLY_RUN_APEX_BRIDGE=true go test -v -timeout 0 -run ^Test_OnlyRunApexBridge$ github.com/0xPolygon/polygon-edge/e2e-polybft/e2e
func Test_OnlyRunApexBridge(t *testing.T) {
	if shouldRun := os.Getenv("ONLY_RUN_APEX_BRIDGE"); shouldRun != "true" {
		t.Skip()
	}

	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
	)

	oracleAPI, err := apex.Bridge.GetBridgingAPI()
	require.NoError(t, err)

	fmt.Printf("oracle API: %s\n", oracleAPI)
	fmt.Printf("oracle API key: %s\n", apiKey)

	fmt.Printf("prime network url: %s\n", apex.PrimeCluster.NetworkURL())
	fmt.Printf("prime ogmios url: %s\n", apex.PrimeCluster.OgmiosURL())
	fmt.Printf("prime bridging addr: %s\n", apex.Bridge.PrimeMultisigAddr)
	fmt.Printf("prime fee addr: %s\n", apex.Bridge.PrimeMultisigFeeAddr)

	fmt.Printf("vector network url: %s\n", apex.VectorCluster.NetworkURL())
	fmt.Printf("vector ogmios url: %s\n", apex.VectorCluster.OgmiosURL())
	fmt.Printf("vector bridging addr: %s\n", apex.Bridge.VectorMultisigAddr)
	fmt.Printf("vector fee addr: %s\n", apex.Bridge.VectorMultisigFeeAddr)

	user := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))
	userPrimeAddr, _, err := wallet.GetWalletAddressCli(user.PrimeWallet, uint(apex.PrimeCluster.Config.NetworkMagic))
	require.NoError(t, err)

	userVectorAddr, _, err := wallet.GetWalletAddressCli(user.VectorWallet, uint(apex.VectorCluster.Config.NetworkMagic))
	require.NoError(t, err)

	primeUserSKHex := hex.EncodeToString(user.PrimeWallet.GetSigningKey())
	vectorUserSKHex := hex.EncodeToString(user.VectorWallet.GetSigningKey())

	fmt.Printf("user prime addr: %s\n", userPrimeAddr)
	fmt.Printf("user prime signing key hex: %s\n", primeUserSKHex)
	fmt.Printf("user vector addr: %s\n", userVectorAddr)
	fmt.Printf("user vector signing key hex: %s\n", vectorUserSKHex)

	signalChannel := make(chan os.Signal, 1)
	// Notify the signalChannel when the interrupt signal is received (Ctrl+C)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	<-signalChannel
}

func TestE2E_ApexBridge_DoNothingWithSpecificUser(t *testing.T) {
	t.Skip()

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(t, ctx)
	user := apex.CreateAndFundExistingUser(
		t, ctx,
		"58201825bce09711e1563fc1702587da6892d1d869894386323bd4378ea5e3d6cba0",
		"5820ccdae0d1cd3fa9be16a497941acff33b9aa20bdbf2f9aa5715942d152988e083", uint64(500_000_000))

	fmt.Println("Prime multisig", apex.Bridge.PrimeMultisigAddr)
	fmt.Println("Prime fee", apex.Bridge.PrimeMultisigFeeAddr)
	fmt.Println("Prime User", user.PrimeAddress)
	fmt.Println("Vector multisig", apex.Bridge.VectorMultisigAddr)
	fmt.Println("Vector fee", apex.Bridge.VectorMultisigFeeAddr)
	fmt.Println("Vector User", user.VectorAddress)

	time.Sleep(time.Second * 60 * 60) // one hour sleep :)
}

func TestE2E_ApexBridge(t *testing.T) {
	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(t, ctx)
	user := apex.CreateAndFundUser(t, ctx, uint64(5_000_000))

	txProviderPrime := apex.GetPrimeTxProvider()
	txProviderVector := apex.GetVectorTxProvider()

	// Initiate bridging PRIME -> VECTOR
	sendAmount := uint64(1_000_000)
	prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
	require.NoError(t, err)

	expectedAmount := prevAmount.Uint64() + sendAmount

	user.BridgeAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
		apex.Bridge.VectorMultisigFeeAddr, sendAmount, true)

	time.Sleep(time.Second * 60)
	err = cardanofw.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
	}, 200, time.Second*20)
	require.NoError(t, err)
}

func TestE2E_ApexBridge_BatchRecreated(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithTTLInc(1),
		cardanofw.WithAPIKey(apiKey),
	)
	user := apex.CreateAndFundUser(t, ctx, uint64(5_000_000))

	txProviderPrime := apex.GetPrimeTxProvider()

	// Initiate bridging PRIME -> VECTOR
	sendAmount := uint64(1_000_000)

	txHash := user.BridgeAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
		apex.Bridge.VectorMultisigFeeAddr, sendAmount, true)

	timeoutTimer := time.NewTimer(time.Second * 300)
	defer timeoutTimer.Stop()

	wentFromFailedOnDestinationToIncludedInBatch := false

	var (
		prevStatus    string
		currentStatus string
	)

	apiURL, err := apex.Bridge.GetBridgingAPI()
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

func TestE2E_ApexBridge_InvalidScenarios(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
	)
	user := apex.CreateAndFundUser(t, ctx, uint64(50_000_000))

	txProviderPrime := apex.GetPrimeTxProvider()

	t.Run("Submitter not enough funds", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.VectorAddress:                sendAmount * 10, // 10Ada
			apex.Bridge.VectorMultisigFeeAddr: feeAmount,
		}

		bridgingRequestMetadata, err := cardanofw.CreateMetaData(
			user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, txHash)
	})

	t.Run("Multiple submitters don't have enough funds", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			sendAmount := uint64(1_000_000)
			feeAmount := uint64(1_100_000)

			receivers := map[string]uint64{
				user.VectorAddress:                sendAmount * 10, // 10Ada
				apex.Bridge.VectorMultisigFeeAddr: feeAmount,
			}

			bridgingRequestMetadata, err := cardanofw.CreateMetaData(
				user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
			require.NoError(t, err)

			txHash, err := cardanofw.SendTx(
				ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
				apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
			require.NoError(t, err)

			apiURL, err := apex.Bridge.GetBridgingAPI()
			require.NoError(t, err)
			cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, txHash)
		}
	})

	t.Run("Multiple submitters don't have enough funds parallel", func(t *testing.T) {
		var err error

		instances := 5
		walletKeys := make([]wallet.IWallet, instances)
		txHashes := make([]string, instances)
		primeGenesisWallet := apex.GetPrimeGenesisWallet(t)

		for i := 0; i < instances; i++ {
			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.PrimeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(walletKeys[i], uint(apex.PrimeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			fundSendAmount := uint64(5_000_000)
			txHash, err := cardanofw.SendTx(ctx, txProviderPrime, primeGenesisWallet,
				fundSendAmount, walletAddress, apex.PrimeCluster.Config.NetworkMagic, []byte{})
			require.NoError(t, err)
			err = wallet.WaitForTxHashInUtxos(ctx, txProviderPrime, walletAddress, txHash,
				60, time.Second*2, cardanofw.IsRecoverableError)
			require.NoError(t, err)
		}

		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		var wg sync.WaitGroup

		for i := 0; i < instances; i++ {
			idx := i
			receivers := map[string]uint64{
				user.VectorAddress:                sendAmount * 10, // 10Ada
				apex.Bridge.VectorMultisigFeeAddr: feeAmount,
			}

			bridgingRequestMetadata, err := cardanofw.CreateMetaData(
				user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
			require.NoError(t, err)

			wg.Add(1)

			go func() {
				defer wg.Done()

				txHashes[idx], err = cardanofw.SendTx(
					ctx, txProviderPrime, walletKeys[idx], (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
					apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
				require.NoError(t, err)
			}()
		}

		wg.Wait()

		for i := 0; i < instances; i++ {
			apiURL, err := apex.Bridge.GetBridgingAPI()
			require.NoError(t, err)
			cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, txHashes[i])
		}
	})

	t.Run("Submitted invalid metadata", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.VectorAddress:                sendAmount,
			apex.Bridge.VectorMultisigFeeAddr: feeAmount,
		}

		bridgingRequestMetadata, err := cardanofw.CreateMetaData(
			user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
		require.NoError(t, err)

		// Send only half bytes of metadata making it invalid
		bridgingRequestMetadata = bridgingRequestMetadata[0 : len(bridgingRequestMetadata)/2]

		_, err = cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.Error(t, err)
	})

	t.Run("Submitted invalid metadata - wrong type", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.VectorAddress:                sendAmount,
			apex.Bridge.VectorMultisigFeeAddr: feeAmount,
		}

		var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0, len(receivers))
		for addr, amount := range receivers {
			transactions = append(transactions, cardanofw.BridgingRequestMetadataTransaction{
				Address: cardanofw.SplitString(addr, 40),
				Amount:  amount,
			})
		}

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t":  "transaction", // should be "bridge"
				"d":  cardanofw.GetDestinationChainID(true),
				"s":  cardanofw.SplitString(user.PrimeAddress, 40),
				"tx": transactions,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

		_, err = cardanofw.WaitForRequestState("InvalidState", ctx, requestURL, apiKey, 60)
		require.Error(t, err)
		require.ErrorContains(t, err, "Timeout")
	})

	t.Run("Submitted invalid metadata - invalid destination", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.VectorAddress:                sendAmount,
			apex.Bridge.VectorMultisigFeeAddr: feeAmount,
		}

		var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0, len(receivers))
		for addr, amount := range receivers {
			transactions = append(transactions, cardanofw.BridgingRequestMetadataTransaction{
				Address: cardanofw.SplitString(addr, 40),
				Amount:  amount,
			})
		}

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t":  "bridge",
				"d":  "", // should be destination chain address
				"s":  cardanofw.SplitString(user.PrimeAddress, 40),
				"tx": transactions,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, txHash)
	})

	t.Run("Submitted invalid metadata - invalid sender", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.VectorAddress:                sendAmount,
			apex.Bridge.VectorMultisigFeeAddr: feeAmount,
		}

		var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0, len(receivers))
		for addr, amount := range receivers {
			transactions = append(transactions, cardanofw.BridgingRequestMetadataTransaction{
				Address: cardanofw.SplitString(addr, 40),
				Amount:  amount,
			})
		}

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t":  "bridge",
				"d":  cardanofw.GetDestinationChainID(true),
				"s":  "", // should be sender address (max len 40)
				"tx": transactions,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, txHash)
	})

	t.Run("Submitted invalid metadata - empty tx", func(t *testing.T) {
		var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0)

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t":  "bridge",
				"d":  cardanofw.GetDestinationChainID(true),
				"s":  cardanofw.SplitString(user.PrimeAddress, 40),
				"tx": transactions, // should not be empty
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, 1_000_000, apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, txHash)
	})
}

func TestE2E_ApexBridge_ValidScenarios(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
	)
	user := apex.CreateAndFundUser(t, ctx, uint64(20_000_000_000))

	txProviderPrime := apex.GetPrimeTxProvider()
	txProviderVector := apex.GetVectorTxProvider()

	primeGenesisWallet := apex.GetPrimeGenesisWallet(t)
	vectorGenesisWallet := apex.GetVectorGenesisWallet(t)

	primeGenesisAddress, _, err := wallet.GetWalletAddressCli(primeGenesisWallet, uint(apex.PrimeCluster.Config.NetworkMagic))
	require.NoError(t, err)

	vectorGenesisAddress, _, err := wallet.GetWalletAddressCli(vectorGenesisWallet, uint(apex.VectorCluster.Config.NetworkMagic))
	require.NoError(t, err)

	fmt.Println("prime genesis addr: ", primeGenesisAddress)
	fmt.Println("vector genesis addr: ", vectorGenesisAddress)
	fmt.Println("prime user addr: ", user.PrimeAddress)
	fmt.Println("vector user addr: ", user.VectorAddress)
	fmt.Println("prime multisig addr: ", apex.Bridge.PrimeMultisigAddr)
	fmt.Println("prime fee addr: ", apex.Bridge.PrimeMultisigFeeAddr)
	fmt.Println("vector multisig addr: ", apex.Bridge.VectorMultisigAddr)
	fmt.Println("vector fee addr: ", apex.Bridge.VectorMultisigFeeAddr)

	t.Run("From prime to vector one by one - wait for other side", func(t *testing.T) {
		instances := 5
		for i := 0; i < instances; i++ {
			sendAmount := uint64(1_000_000)
			prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
			require.NoError(t, err)

			fmt.Printf("%v - prevAmount %v\n", i+1, prevAmount)

			txHash := user.BridgeAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				apex.Bridge.VectorMultisigFeeAddr, sendAmount, true)

			fmt.Printf("%v - Tx sent. hash: %s\n", i+1, txHash)

			expectedAmount := prevAmount.Uint64() + sendAmount
			fmt.Printf("%v - expectedAmount %v\n", i+1, expectedAmount)

			err = cardanofw.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
				return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
			}, 20, time.Second*10)
			require.NoError(t, err)
		}
	})

	t.Run("From prime to vector one by one", func(t *testing.T) {
		instances := 5
		sendAmount := uint64(1_000_000)
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash := user.BridgeAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				apex.Bridge.VectorMultisigFeeAddr, sendAmount, true)

			fmt.Printf("Tx %v sent. hash: %s\n", i+1, txHash)
		}

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
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
			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.PrimeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(walletKeys[i], uint(apex.PrimeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			fundSendAmount := uint64(5_000_000)
			user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, fundSendAmount, walletAddress, true)
		}

		sendAmount := uint64(1_000_000)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(apex.PrimeCluster.Config.NetworkMagic),
					apex.Bridge.PrimeMultisigAddr, apex.Bridge.VectorMultisigFeeAddr, walletKeys[idx], user.VectorAddress, sendAmount,
					cardanofw.GetDestinationChainID(true))
				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", instances)

		newAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("prevAmount: %v. newAmount: %v\n", prevAmount, newAmount)
	})

	t.Run("From vector to prime one by one", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		instances := 5
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash := user.BridgeAmount(t, ctx, txProviderVector, apex.Bridge.VectorMultisigAddr,
				apex.Bridge.PrimeMultisigFeeAddr, sendAmount, false)

			fmt.Printf("Tx %v sent. hash: %s\n", i+1, txHash)
		}

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val *big.Int) bool {
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
			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.VectorCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(walletKeys[i], uint(apex.VectorCluster.Config.NetworkMagic))
			require.NoError(t, err)

			fundSendAmount := uint64(5_000_000)
			user.SendToAddress(t, ctx, txProviderVector, vectorGenesisWallet, fundSendAmount, walletAddress, true)
		}

		sendAmount := uint64(1_000_000)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderVector, uint(apex.VectorCluster.Config.NetworkMagic),
					apex.Bridge.VectorMultisigAddr, apex.Bridge.PrimeMultisigFeeAddr, walletKeys[idx], user.PrimeAddress, sendAmount,
					cardanofw.GetDestinationChainID(false))
				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		expectedAmount := prevAmount.Uint64() + uint64(instances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", instances)

		newAmount, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		fmt.Printf("prevAmount: %v. newAmount: %v\n", prevAmount, newAmount)
	})

	t.Run("From prime to vector sequential and parallel", func(t *testing.T) {
		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		sequentialInstances := 5
		parallelInstances := 10
		sendAmount := uint64(1_000_000)

		var wg sync.WaitGroup

		for j := 0; j < sequentialInstances; j++ {
			walletKeys := make([]wallet.IWallet, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.PrimeCluster.Config.Dir("keys")), true)
				require.NoError(t, err)

				walletAddress, _, err := wallet.GetWalletAddressCli(walletKeys[i], uint(apex.PrimeCluster.Config.NetworkMagic))
				require.NoError(t, err)

				fundSendAmount := uint64(5_000_000)
				user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, fundSendAmount, walletAddress, true)
			}

			fmt.Printf("run: %v. Funded %v wallets \n", j+1, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(apex.PrimeCluster.Config.NetworkMagic),
						apex.Bridge.PrimeMultisigAddr, apex.Bridge.VectorMultisigFeeAddr, walletKeys[idx], user.VectorAddress, sendAmount,
						cardanofw.GetDestinationChainID(true))
					fmt.Printf("run: %v. Tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()
		}

		fmt.Printf("Waiting for %v TXs\n", sequentialInstances*parallelInstances)

		expectedAmount := prevAmount.Uint64() + uint64(sequentialInstances)*uint64(parallelInstances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmount)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", sequentialInstances*parallelInstances)

		newAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("prevAmount: %v. newAmount: %v\n", prevAmount, newAmount)
	})

	//nolint:dupl
	t.Run("From prime to vector 200x 5min 90%", func(t *testing.T) {
		instances := 200
		maxWaitTime := 300
		fundSendAmount := uint64(5_000_000)
		sendAmount := uint64(1_000_000)
		successChance := 90 // 90%
		succeededCount := 0
		walletKeys := make([]wallet.IWallet, instances)

		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("Funding %v Wallets\n", instances)

		for i := 0; i < instances; i++ {
			if (i+1)%100 == 0 {
				fmt.Printf("Funded %v..%v\n", i-99, i)
			}

			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.PrimeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(walletKeys[i], uint(apex.PrimeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, fundSendAmount, walletAddress, true)
		}

		fmt.Printf("Funding Complete\n")

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCount++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(apex.PrimeCluster.Config.NetworkMagic),
						apex.Bridge.PrimeMultisigAddr, apex.Bridge.VectorMultisigFeeAddr, walletKeys[idx], user.VectorAddress, sendAmount,
						cardanofw.GetDestinationChainID(true))
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.VectorAddress:                sendAmount * 10, // 10Ada
						apex.Bridge.VectorMultisigFeeAddr: feeAmount,
					}

					bridgingRequestMetadata, err := cardanofw.CreateMetaData(
						user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderPrime, walletKeys[idx], (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
						apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := prevAmount.Uint64() + uint64(succeededCount)*sendAmount

		var newAmount *big.Int
		for i := 0; i < instances; i++ {
			newAmount, err = cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
			require.NoError(t, err)
			fmt.Printf("Current Amount Prime: %v. Expected: %v\n", newAmount, expectedAmount)

			if newAmount.Uint64() == expectedAmount {
				break
			}

			time.Sleep(time.Second * 10)
		}

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, prevAmount, newAmount, expectedAmount)
	})

	//nolint:dupl
	t.Run("From prime to vector 1000x 20min 90%", func(t *testing.T) {
		instances := 1000
		maxWaitTime := 1200
		fundSendAmount := uint64(5_000_000)
		sendAmount := uint64(1_000_000)
		successChance := 90 // 90%
		succeededCount := 0
		walletKeys := make([]wallet.IWallet, instances)

		prevAmount, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("Funding %v Wallets\n", instances)

		for i := 0; i < instances; i++ {
			if (i+1)%100 == 0 {
				fmt.Printf("Funded %v..%v\n", i-99, i)
			}

			walletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.PrimeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(walletKeys[i], uint(apex.PrimeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, fundSendAmount, walletAddress, true)
		}

		fmt.Printf("Funding Complete\n")

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCount++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(apex.PrimeCluster.Config.NetworkMagic),
						apex.Bridge.PrimeMultisigAddr, apex.Bridge.VectorMultisigFeeAddr, walletKeys[idx], user.VectorAddress, sendAmount,
						cardanofw.GetDestinationChainID(true))
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.VectorAddress:                sendAmount * 10, // 10Ada+
						apex.Bridge.VectorMultisigFeeAddr: feeAmount,
					}

					bridgingRequestMetadata, err := cardanofw.CreateMetaData(
						user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderPrime, walletKeys[idx], (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
						apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := prevAmount.Uint64() + uint64(succeededCount)*sendAmount

		var newAmount *big.Int
		for i := 0; i < instances; i++ {
			newAmount, err = cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
			require.NoError(t, err)
			fmt.Printf("Current Amount Prime: %v. Expected: %v\n", newAmount, expectedAmount)

			if newAmount.Uint64() == expectedAmount {
				break
			}

			time.Sleep(time.Second * 10)
		}

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, prevAmount, newAmount.Uint64(), expectedAmount)
	})

	t.Run("Both directions 1000x 60min 90%", func(t *testing.T) {
		instances := 1000
		maxWaitTime := 3600
		fundSendAmount := uint64(5_000_000)
		sendAmount := uint64(1_000_000)
		successChance := 90 // 90%
		succeededCountPrime := 0
		succeededCountVector := 0
		walletKeysPrime := make([]wallet.IWallet, instances)
		walletKeysVector := make([]wallet.IWallet, instances)

		prevAmountPrime, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)
		prevAmountVector, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		fmt.Printf("Funding %v Wallets\n", instances)

		for i := 0; i < instances; i++ {
			if (i+1)%100 == 0 {
				fmt.Printf("Funded %v..%v on prime\n", i-99, i)
			}

			walletKeysPrime[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.PrimeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(walletKeysPrime[i], uint(apex.PrimeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, fundSendAmount, walletAddress, true)
		}

		for i := 0; i < instances; i++ {
			if (i+1)%100 == 0 {
				fmt.Printf("Funded %v..%v on vector\n", i-99, i)
			}

			walletKeysVector[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.VectorCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(walletKeysVector[i], uint(apex.VectorCluster.Config.NetworkMagic))
			require.NoError(t, err)

			user.SendToAddress(t, ctx, txProviderVector, vectorGenesisWallet, fundSendAmount, walletAddress, false)
		}

		fmt.Printf("Funding Complete\n")

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(2)

			//nolint:dupl
			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCountPrime++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(apex.PrimeCluster.Config.NetworkMagic),
						apex.Bridge.PrimeMultisigAddr, apex.Bridge.VectorMultisigFeeAddr, walletKeysPrime[idx], user.VectorAddress, sendAmount,
						cardanofw.GetDestinationChainID(true))
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.VectorAddress:                sendAmount * 10, // 10Ada+
						apex.Bridge.VectorMultisigFeeAddr: feeAmount,
					}

					bridgingRequestMetadata, err := cardanofw.CreateMetaData(
						user.PrimeAddress, receivers, cardanofw.GetDestinationChainID(true))
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderPrime, walletKeysPrime[idx], (sendAmount + feeAmount), apex.Bridge.PrimeMultisigAddr,
						apex.PrimeCluster.Config.NetworkMagic, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)

			//nolint:dupl
			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCountVector++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					cardanofw.BridgeAmountFull(t, ctx, txProviderVector, uint(apex.VectorCluster.Config.NetworkMagic),
						apex.Bridge.VectorMultisigAddr, apex.Bridge.PrimeMultisigFeeAddr, walletKeysVector[idx], user.PrimeAddress, sendAmount,
						cardanofw.GetDestinationChainID(false))
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.PrimeAddress:                sendAmount * 10, // 10Ada+
						apex.Bridge.PrimeMultisigFeeAddr: feeAmount,
					}

					bridgingRequestMetadata, err := cardanofw.CreateMetaData(
						user.VectorAddress, receivers, cardanofw.GetDestinationChainID(false))
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderVector, walletKeysVector[idx], (sendAmount + feeAmount), apex.Bridge.VectorMultisigAddr,
						apex.VectorCluster.Config.NetworkMagic, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmountPrime := prevAmountPrime.Uint64() + uint64(succeededCountPrime)*sendAmount
		expectedAmountVector := prevAmountVector.Uint64() + uint64(succeededCountVector)*sendAmount

		for i := 0; i < instances; i++ {
			newAmountPrime, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
			require.NoError(t, err)
			fmt.Printf("Current Amount Prime: %v. Expected: %v\n", newAmountPrime, expectedAmountPrime)

			newAmountVector, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
			require.NoError(t, err)
			fmt.Printf("Current Amount Vector: %v. Expected: %v\n", newAmountVector, expectedAmountVector)

			if newAmountPrime.Uint64() == expectedAmountPrime && newAmountVector.Uint64() == expectedAmountVector {
				break
			}

			time.Sleep(time.Second * 10)
		}

		newAmountPrime, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)
		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCountPrime, prevAmountPrime, newAmountPrime, expectedAmountPrime)

		newAmountVector, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)
		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCountVector, prevAmountVector, newAmountVector, expectedAmountVector)
	})

	t.Run("Both directions sequential", func(t *testing.T) {
		instances := 5
		sendAmount := uint64(1_000_000)

		for i := 0; i < instances; i++ {
			primeTxHash := user.BridgeAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				apex.Bridge.VectorMultisigFeeAddr, sendAmount, true)

			fmt.Printf("prime tx %v sent. hash: %s\n", i+1, primeTxHash)

			vectorTxHash := user.BridgeAmount(t, ctx, txProviderVector, apex.Bridge.VectorMultisigAddr,
				apex.Bridge.PrimeMultisigFeeAddr, sendAmount, false)

			fmt.Printf("vector tx %v sent. hash: %s\n", i+1, vectorTxHash)
		}

		prevAmountOnVector, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)
		prevAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		fmt.Printf("Waiting for %v TXs on vector\n", instances)
		expectedAmountOnVector := prevAmountOnVector.Uint64() + uint64(instances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmountOnVector)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)

		newAmountOnVector, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("on vector: prevAmount: %v. newAmount: %v\n", prevAmountOnVector, newAmountOnVector)

		fmt.Printf("Waiting for %v TXs on prime\n", instances)
		expectedAmountOnPrime := prevAmountOnPrime.Uint64() + uint64(instances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmountOnPrime)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)

		newAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		fmt.Printf("on prime: prevAmount: %v. newAmount: %v\n", prevAmountOnPrime, newAmountOnPrime)
	})

	t.Run("Both directions sequential and parallel", func(t *testing.T) {
		sequentialInstances := 5
		parallelInstances := 6

		primeWalletKeys := make([]wallet.IWallet, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			primeWalletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.PrimeCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(primeWalletKeys[i], uint(apex.PrimeCluster.Config.NetworkMagic))
			require.NoError(t, err)

			fundSendAmount := uint64(5_000_000)
			user.SendToAddress(t, ctx, txProviderPrime, primeGenesisWallet, uint64(sequentialInstances)*fundSendAmount, walletAddress, true)
		}

		fmt.Printf("Funded %v prime wallets \n", parallelInstances)

		vectorWalletKeys := make([]wallet.IWallet, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			vectorWalletKeys[i], err = wallet.NewStakeWalletManager().Create(path.Join(apex.VectorCluster.Config.Dir("keys")), true)
			require.NoError(t, err)

			walletAddress, _, err := wallet.GetWalletAddressCli(vectorWalletKeys[i], uint(apex.VectorCluster.Config.NetworkMagic))
			require.NoError(t, err)

			fundSendAmount := uint64(5_000_000)
			user.SendToAddress(t, ctx, txProviderVector, vectorGenesisWallet, uint64(sequentialInstances)*fundSendAmount, walletAddress, false)
		}

		fmt.Printf("Funded %v vector wallets \n", parallelInstances)

		prevAmountOnVector, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)
		prevAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		sendAmount := uint64(1_000_000)

		for j := 0; j < sequentialInstances; j++ {
			var wg sync.WaitGroup

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderPrime, uint(apex.PrimeCluster.Config.NetworkMagic),
						apex.Bridge.PrimeMultisigAddr, apex.Bridge.VectorMultisigFeeAddr, primeWalletKeys[idx], user.VectorAddress, sendAmount,
						cardanofw.GetDestinationChainID(true))
					fmt.Printf("run: %v. Prime tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := cardanofw.BridgeAmountFull(t, ctx, txProviderVector, uint(apex.VectorCluster.Config.NetworkMagic),
						apex.Bridge.VectorMultisigAddr, apex.Bridge.PrimeMultisigFeeAddr, vectorWalletKeys[idx], user.PrimeAddress, sendAmount,
						cardanofw.GetDestinationChainID(false))
					fmt.Printf("run: %v. Vector tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()
		}

		fmt.Printf("Waiting for %v TXs on vector:\n", sequentialInstances*parallelInstances)

		expectedAmountOnVector := prevAmountOnVector.Uint64() + uint64(sequentialInstances)*uint64(parallelInstances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderVector, user.VectorAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmountOnVector)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs on vector confirmed\n", sequentialInstances*parallelInstances)

		newAmountOnVector, err := cardanofw.GetTokenAmount(ctx, txProviderVector, user.VectorAddress)
		require.NoError(t, err)

		fmt.Printf("on vector: prevAmount: %v. newAmount: %v\n", prevAmountOnVector, newAmountOnVector)

		fmt.Printf("Waiting for %v TXs on prime\n", sequentialInstances*parallelInstances)

		expectedAmountOnPrime := prevAmountOnPrime.Uint64() + uint64(sequentialInstances)*uint64(parallelInstances)*sendAmount
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val *big.Int) bool {
			return val.Cmp(new(big.Int).SetUint64(expectedAmountOnPrime)) == 0
		}, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs on prime confirmed\n", sequentialInstances*parallelInstances)

		newAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)

		fmt.Printf("on prime: prevAmount: %v. newAmount: %v\n", prevAmountOnPrime, newAmountOnPrime)
	})
}
