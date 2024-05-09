package cardanofw

import (
	"context"
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
) ([]*TestCardanoCluster, func()) {
	t.Helper()

	var (
		errors      = make([]error, clusterCnt)
		clusters    = make([]*TestCardanoCluster, clusterCnt)
		wg          sync.WaitGroup
		baseLogsDir = path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().UTC().Unix()), t.Name())
	)

	cleanupFunc := func() {
		for i := 0; i < clusterCnt; i++ {
			if clusters[i] != nil {
				clusters[i].Stop() //nolint:errcheck
			}
		}
	}

	t.Cleanup(cleanupFunc)

	for i := 0; i < clusterCnt; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			checkAndSetError := func(err error) bool {
				errors[id] = err

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

			ctx, cncl := context.WithCancel(context.Background())
			defer cncl()

			if errors[id] = cluster.WaitForReady(time.Minute * 2); errors[id] != nil {
				return
			}

			err = cluster.StartOgmios(t)
			if checkAndSetError(err) {
				return
			}

			txProvider := wallet.NewTxProviderOgmios(cluster.OgmiosURL())

			if errors[id] = cluster.WaitForBlockWithState(10, time.Second*120); errors[id] != nil {
				return
			}

			errors[id] = WaitUntilBlock(t, ctx, txProvider, 15, time.Second*120)

			fmt.Printf("Cluster %d is ready\n", id)
		}(i)
	}

	wg.Wait()

	for i := 0; i < clusterCnt; i++ {
		assert.NoError(t, errors[i])
	}

	return clusters, cleanupFunc
}

func SetupAndRunApexBridge(
	t *testing.T,
	ctx context.Context,
	dataDir string,
	bladeValidatorsNum int,
	primeCluster *TestCardanoCluster,
	vectorCluster *TestCardanoCluster,
) (*TestCardanoBridge, func()) {
	t.Helper()

	const (
		sendAmount     = uint64(10_000_000)
		bladeEpochSize = 5
		numOfRetries   = 90
		waitTime       = time.Second * 2
	)

	cleanupDataDir := func() {
		os.RemoveAll(dataDir)
	}

	cleanupDataDir()

	cb := NewTestCardanoBridge(dataDir, bladeValidatorsNum)

	cleanupFunc := func() {
		// cleanupDataDir()
		cb.StopValidators()
	}

	t.Cleanup(cleanupFunc)

	require.NoError(t, cb.CardanoCreateWalletsAndAddresses(
		primeCluster.Config.NetworkMagic, vectorCluster.Config.NetworkMagic))

	fmt.Printf("Wallets and addresses created\n")

	txProviderPrime := wallet.NewTxProviderOgmios(primeCluster.OgmiosURL())
	txProviderVector := wallet.NewTxProviderOgmios(vectorCluster.OgmiosURL())

	primeGenesisWallet, err := GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 1)
	require.NoError(t, err)

	require.NoError(t, SendTx(ctx, txProviderPrime, primeGenesisWallet, sendAmount,
		cb.PrimeMultisigAddr, primeCluster.Config.NetworkMagic, []byte{}))

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, numOfRetries, waitTime)
	require.NoError(t, err)

	fmt.Printf("Prime multisig addr funded\n")

	require.NoError(t, SendTx(ctx, txProviderPrime, primeGenesisWallet, sendAmount,
		cb.PrimeMultisigFeeAddr, primeCluster.Config.NetworkMagic, []byte{}))

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigFeeAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, numOfRetries, waitTime)
	require.NoError(t, err)

	fmt.Printf("Prime multisig fee addr funded\n")

	vectorGenesisWallet, err := GetGenesisWalletFromCluster(vectorCluster.Config.TmpDir, 1)
	require.NoError(t, err)

	require.NoError(t, SendTx(ctx, txProviderVector, vectorGenesisWallet, sendAmount,
		cb.VectorMultisigAddr, vectorCluster.Config.NetworkMagic, []byte{}))

	err = wallet.WaitForAmount(context.Background(), txProviderVector, cb.VectorMultisigAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, numOfRetries, waitTime)
	require.NoError(t, err)

	fmt.Printf("Vector multisig addr funded\n")

	require.NoError(t, SendTx(ctx, txProviderVector, vectorGenesisWallet, sendAmount,
		cb.VectorMultisigFeeAddr, vectorCluster.Config.NetworkMagic, []byte{}))

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
		40000,
		"test_api_key",
	))

	fmt.Printf("Configs generated\n")

	require.NoError(t, cb.StartValidatorComponents(ctx))
	fmt.Printf("Validator components started\n")

	require.NoError(t, cb.StartRelayer(ctx))
	fmt.Printf("Relayer started\n")

	return cb, cleanupFunc
}
