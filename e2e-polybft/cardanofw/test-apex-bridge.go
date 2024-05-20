package cardanofw

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func SetupAndRunApexCardanoChains(
	t *testing.T,
	ctx context.Context,
	clusterCnt int,
) []*TestCardanoCluster {
	t.Helper()

	var (
		clErrors    = make([]error, clusterCnt)
		clusters    = make([]*TestCardanoCluster, clusterCnt)
		wg          sync.WaitGroup
		baseLogsDir = path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().UTC().Unix()), t.Name())
	)

	cleanupFunc := func() {
		fmt.Printf("Cleaning up cardano chains\n")

		wg := sync.WaitGroup{}
		stopErrs := []error(nil)

		for i := 0; i < clusterCnt; i++ {
			if clusters[i] != nil {
				wg.Add(1)

				go func(cl *TestCardanoCluster) {
					defer wg.Done()

					stopErrs = append(stopErrs, cl.Stop())
				}(clusters[i])
			}
		}

		wg.Wait()

		fmt.Printf("Done cleaning up cardano chains: %v\n", errors.Join(stopErrs...))
	}

	t.Cleanup(cleanupFunc)

	for i := 0; i < clusterCnt; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			checkAndSetError := func(err error) bool {
				clErrors[id] = err

				return err != nil
			}

			logsDir := fmt.Sprintf("%s/%d", baseLogsDir, id)

			err := common.CreateDirSafe(logsDir, 0750)
			if checkAndSetError(err) {
				return
			}

			cluster, err := NewCardanoTestCluster(t,
				WithID(id+1),
				WithNodesCount(4),
				WithStartTimeDelay(time.Second*5),
				WithPort(5000+id*100),
				WithOgmiosPort(1337+id),
				WithLogsDir(logsDir),
				WithNetworkMagic(42+id),
			)
			if checkAndSetError(err) {
				return
			}

			cluster.Config.WithStdout = false
			clusters[id] = cluster

			fmt.Printf("Waiting for sockets to be ready\n")

			if checkAndSetError(cluster.WaitForReady(time.Minute * 2)) {
				return
			}

			if checkAndSetError(cluster.StartOgmios(t)) {
				return
			}

			if checkAndSetError(cluster.WaitForBlockWithState(10, time.Second*120)) {
				return
			}

			fmt.Printf("Cluster %d is ready\n", id)
		}(i)
	}

	wg.Wait()

	for i := 0; i < clusterCnt; i++ {
		assert.NoError(t, clErrors[i])
	}

	return clusters
}

func SetupAndRunApexBridge(
	t *testing.T,
	ctx context.Context,
	dataDir string,
	bladeValidatorsNum int,
	primeCluster *TestCardanoCluster,
	vectorCluster *TestCardanoCluster,
	opts ...CardanoBridgeOption,
) *TestCardanoBridge {
	t.Helper()

	const (
		sendAmount     = uint64(100_000_000_000)
		bladeEpochSize = 5
		numOfRetries   = 90
		waitTime       = time.Second * 2
		apiPort        = 40000
		apiKey         = "test_api_key"
	)

	cleanupDataDir := func() {
		os.RemoveAll(dataDir)
	}

	cleanupDataDir()

	opts = append(opts,
		WithAPIPortStart(apiPort),
		WithAPIKey(apiKey),
	)

	cb := NewTestCardanoBridge(dataDir, bladeValidatorsNum, opts...)

	cleanupFunc := func() {
		fmt.Printf("Cleaning up apex bridge\n")

		// cleanupDataDir()
		cb.StopValidators()

		fmt.Printf("Done cleaning up apex bridge\n")
	}

	t.Cleanup(cleanupFunc)

	require.NoError(t, cb.CardanoCreateWalletsAndAddresses(
		primeCluster.Config.NetworkMagic, vectorCluster.Config.NetworkMagic))

	fmt.Printf("Wallets and addresses created\n")

	txProviderPrime := wallet.NewTxProviderOgmios(primeCluster.OgmiosURL())
	txProviderVector := wallet.NewTxProviderOgmios(vectorCluster.OgmiosURL())

	primeGenesisWallet, err := GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 1)
	require.NoError(t, err)

	_, err = SendTx(ctx, txProviderPrime, primeGenesisWallet, sendAmount,
		cb.PrimeMultisigAddr, primeCluster.Config.NetworkMagic, []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, numOfRetries, waitTime)
	require.NoError(t, err)

	fmt.Printf("Prime multisig addr funded\n")

	_, err = SendTx(ctx, txProviderPrime, primeGenesisWallet, sendAmount,
		cb.PrimeMultisigFeeAddr, primeCluster.Config.NetworkMagic, []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigFeeAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, numOfRetries, waitTime)
	require.NoError(t, err)

	fmt.Printf("Prime multisig fee addr funded\n")

	vectorGenesisWallet, err := GetGenesisWalletFromCluster(vectorCluster.Config.TmpDir, 1)
	require.NoError(t, err)

	_, err = SendTx(ctx, txProviderVector, vectorGenesisWallet, sendAmount,
		cb.VectorMultisigAddr, vectorCluster.Config.NetworkMagic, []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderVector, cb.VectorMultisigAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, numOfRetries, waitTime)
	require.NoError(t, err)

	fmt.Printf("Vector multisig addr funded\n")

	_, err = SendTx(ctx, txProviderVector, vectorGenesisWallet, sendAmount,
		cb.VectorMultisigFeeAddr, vectorCluster.Config.NetworkMagic, []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderVector, cb.VectorMultisigFeeAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, numOfRetries, waitTime)
	require.NoError(t, err)

	fmt.Printf("Vector multisig fee addr funded\n")

	cb.StartValidators(t, bladeEpochSize)

	fmt.Printf("Validators started\n")

	cb.WaitForValidatorsReady(t)

	fmt.Printf("Validators ready\n")

	// need params for it to work properly
	primeTokenSupply := big.NewInt(int64(sendAmount))
	vectorTokenSupply := big.NewInt(int64(sendAmount))
	require.NoError(t, cb.RegisterChains(
		primeTokenSupply,
		primeCluster.OgmiosURL(),
		vectorTokenSupply,
		vectorCluster.OgmiosURL(),
	))

	fmt.Printf("Chain registered\n")

	// need params for it to work properly
	require.NoError(t, cb.GenerateConfigs(
		primeCluster.NetworkURL(),
		primeCluster.Config.NetworkMagic,
		primeCluster.OgmiosURL(),
		vectorCluster.NetworkURL(),
		vectorCluster.Config.NetworkMagic,
		vectorCluster.OgmiosURL(),
	))

	fmt.Printf("Configs generated\n")

	require.NoError(t, cb.StartValidatorComponents(ctx))
	fmt.Printf("Validator components started\n")

	require.NoError(t, cb.StartRelayer(ctx))
	fmt.Printf("Relayer started\n")

	return cb
}
