package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"path"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
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

			err = cardanofw.SendTx(ctx, txProviders[i], genesisWallet,
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

	primeWalletKeys, err := wallet.NewStakeWalletManager().Create(path.Join(primeCluster.Config.Dir("keys")), true)
	require.NoError(t, err)

	primeUserAddress, _, err := wallet.GetWalletAddress(primeWalletKeys, uint(primeCluster.Config.NetworkMagic))
	require.NoError(t, err)

	vectorWalletKeys, err := wallet.NewStakeWalletManager().Create(path.Join(vectorCluster.Config.Dir("keys")), true)
	require.NoError(t, err)

	vectorUserAddress, _, err := wallet.GetWalletAddress(vectorWalletKeys, uint(vectorCluster.Config.NetworkMagic))
	require.NoError(t, err)

	txProviderPrime := wallet.NewTxProviderOgmios(primeCluster.OgmiosURL())
	txProviderVector := wallet.NewTxProviderOgmios(vectorCluster.OgmiosURL())

	// Fund prime address
	primeGenesisWallet, err := cardanofw.GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	sendAmount := uint64(5_000_000)
	require.NoError(t, cardanofw.SendTx(ctx, txProviderPrime, primeGenesisWallet,
		sendAmount, primeUserAddress, primeCluster.Config.NetworkMagic, []byte{}))

	require.NoError(t, wallet.WaitForAmount(context.Background(), txProviderPrime, primeUserAddress, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 60, time.Second*2))

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
	var receivers = make(map[string]uint64, 2)

	sendAmount = uint64(1_000_000)

	receivers[vectorUserAddress] = sendAmount
	receivers[cb.VectorMultisigFeeAddr] = 1_100_000

	bridgingRequestMetadata, err := CreateMetaData(primeUserAddress, receivers)
	require.NoError(t, err)

	require.NoError(t, cardanofw.SendTx(
		ctx, txProviderPrime, primeWalletKeys, 2_100_000, cb.PrimeMultisigAddr,
		primeCluster.Config.NetworkMagic, bridgingRequestMetadata))

	err = wallet.WaitForAmount(context.Background(), txProviderVector, vectorUserAddress, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 100, time.Minute*5)
	require.NoError(t, err)

	fmt.Printf("Prime address = " + primeUserAddress)
	fmt.Printf("\n")
	fmt.Printf("Vector address = " + vectorUserAddress)
	fmt.Printf("\n")
}

func CreateMetaData(sender string, receivers map[string]uint64) ([]byte, error) {
	type BridgingRequestMetadataTransaction struct {
		Address []string `cbor:"a" json:"a"`
		Amount  uint64   `cbor:"m" json:"m"`
	}

	var transactions = make([]BridgingRequestMetadataTransaction, 0, len(receivers))
	for addr, amount := range receivers {
		transactions = append(transactions, BridgingRequestMetadataTransaction{
			Address: cardanofw.SplitString(addr, 40),
			Amount:  amount,
		})
	}

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"t":  "bridge",
			"d":  "vector",
			"s":  cardanofw.SplitString(sender, 40),
			"tx": transactions,
		},
	}

	return json.Marshal(metadata)
}
