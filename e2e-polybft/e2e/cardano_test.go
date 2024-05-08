package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Download Cardano executables from https://github.com/IntersectMBO/cardano-node/releases/tag/8.7.3 and unpack tar.gz file
// Add directory where unpacked files are located to the $PATH (in example bellow `~/Apps/cardano`)
// eq add line `export PATH=$PATH:~/Apps/cardano` to  `~/.bashrc`
func TestE2E_CardanoTwoClustersBasic(t *testing.T) {
	const (
		clusterCnt = 2
	)

	var (
		errors      [clusterCnt]error
		wg          sync.WaitGroup
		baseLogsDir = path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().UTC().Unix()), t.Name())
	)

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

			cluster, err := cardanofw.NewCardanoTestCluster(t,
				cardanofw.WithID(id+1),
				cardanofw.WithNodesCount(4),
				cardanofw.WithStartTimeDelay(time.Second*5),
				cardanofw.WithPort(5000+id*100),
				cardanofw.WithOgmiosPort(1337+id),
				cardanofw.WithLogsDir(logsDir),
				cardanofw.WithNetworkMagic(42+id))
			if checkAndSetError(err) {
				return
			}

			err = cluster.StartDocker()
			if checkAndSetError(err) {
				return
			}

			defer cluster.StopDocker() //nolint:errcheck

			t.Log("Waiting for sockets to be ready")

			txProvider := wallet.NewOgmiosProvider(cluster.OgmiosURL())

			errors[id] = cardanofw.WaitUntilBlock(t, context.Background(), txProvider, 4, time.Second*120)
			t.Run("simple send", func(t *testing.T) {
				newWalletKeys, err := wallet.NewStakeWalletManager().Create(path.Join(cluster.Config.Dir("keys")), true)
				if checkAndSetError(err) {
					return
				}

				receiver, _, err := wallet.GetWalletAddress(newWalletKeys, uint(cluster.Config.NetworkMagic))
				if checkAndSetError(err) {
					return
				}

				ctx, cncl := context.WithCancel(context.Background())
				defer cncl()

				sendAmount := uint64(1000000)

				genesisWallet, err := cardanofw.GetGenesisWalletFromCluster(cluster.Config.TmpDir, 1)
				if checkAndSetError(err) {
					return
				}

				err = cardanofw.SendTx(ctx, txProvider, genesisWallet,
					sendAmount, receiver, cluster.Config.NetworkMagic, []byte{})
				if checkAndSetError(err) {
					return
				}

				err = wallet.WaitForAmount(context.Background(), txProvider, receiver, func(val *big.Int) bool {
					return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
				}, 60, time.Second*2)
				if checkAndSetError(err) {
					return
				}
			})
		}(i)
	}

	wg.Wait()

	for i := 0; i < clusterCnt; i++ {
		assert.NoError(t, errors[i])
	}
}

func TestE2E_ApexBridge(t *testing.T) {
	const (
		clusterCnt         = 2
		bladeValidatorsNum = 4
		bladeEpochSize     = 5
	)

	var (
		errors        [clusterCnt]error
		wg            sync.WaitGroup
		baseLogsDir   = path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().UTC().Unix()), t.Name())
		primeCluster  *cardanofw.TestCardanoCluster
		vectorCluster *cardanofw.TestCardanoCluster
	)

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

			cluster, err := cardanofw.NewCardanoTestCluster(t,
				cardanofw.WithID(id+1),
				cardanofw.WithNodesCount(4),
				cardanofw.WithStartTimeDelay(time.Second*5),
				cardanofw.WithPort(5000+id*100),
				cardanofw.WithOgmiosPort(1337+id),
				cardanofw.WithLogsDir(logsDir),
				cardanofw.WithNetworkMagic(42+id))
			if checkAndSetError(err) {
				return
			}

			if id == 0 {
				primeCluster = cluster
			} else {
				vectorCluster = cluster
			}

			err = cluster.StartDocker()
			if checkAndSetError(err) {
				return
			}

			fmt.Printf("Waiting for sockets to be ready\n")

			txProvider := wallet.NewOgmiosProvider(cluster.OgmiosURL())

			errors[id] = cardanofw.WaitUntilBlock(t, context.Background(), txProvider, 4, time.Second*120)

			fmt.Printf("Cluster %d is ready\n", id)
		}(i)
	}

	wg.Wait()

	for i := 0; i < clusterCnt; i++ {
		assert.NoError(t, errors[i])
	}

	defer primeCluster.StopDocker()  //nolint:errcheck
	defer vectorCluster.StopDocker() //nolint:errcheck

	primeWalletKeys, err := wallet.NewStakeWalletManager().Create(path.Join(primeCluster.Config.Dir("keys")), true)
	require.NoError(t, err)

	primeUserAddress, _, err := wallet.GetWalletAddress(primeWalletKeys, uint(primeCluster.Config.NetworkMagic))
	require.NoError(t, err)

	vectorWalletKeys, err := wallet.NewStakeWalletManager().Create(path.Join(vectorCluster.Config.Dir("keys")), true)
	require.NoError(t, err)

	vectorUserAddress, _, err := wallet.GetWalletAddress(vectorWalletKeys, uint(vectorCluster.Config.NetworkMagic))
	require.NoError(t, err)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	txProviderPrime := wallet.NewOgmiosProvider(primeCluster.OgmiosURL())
	txProviderVector := wallet.NewOgmiosProvider(vectorCluster.OgmiosURL())

	// Fund prime address
	primeGenesisWallet, err := cardanofw.GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	sendAmount := uint64(3_000_000)
	require.NoError(t, cardanofw.SendTx(ctx, txProviderPrime, primeGenesisWallet,
		sendAmount, primeUserAddress, primeCluster.Config.NetworkMagic, []byte{}))

	require.NoError(t, wallet.WaitForAmount(context.Background(), txProviderPrime, primeUserAddress, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 60, time.Second*2))

	fmt.Printf("Prime user address funded\n")

	cb, fun := cardanofw.SetupAndRunApexBridge(t,
		ctx,
		path.Join(path.Dir(primeCluster.Config.TmpDir), "bridge"),
		bladeValidatorsNum,
		bladeEpochSize,
		"http://localhost:5001",
		primeCluster.Config.NetworkMagic,
		primeCluster.OgmiosURL(),
		"http://localhost:5101",
		vectorCluster.Config.NetworkMagic,
		vectorCluster.OgmiosURL())
	defer fun()

	fmt.Printf("Apex bridge setup done\n")

	// Initiate bridging PRIME -> VECTOR
	var receivers = make(map[string]uint64, 2)

	sendAmount = uint64(1_000_000)

	receivers[vectorUserAddress] = sendAmount
	receivers[cb.PrimeMultisigFeeAddr] = 1_100_000

	bridgingRequestMetadata, err := CreateMetaData(primeUserAddress, receivers)
	require.NoError(t, err)

	require.NoError(t, cardanofw.SendTx(ctx, txProviderPrime, primeWalletKeys, 2_100_000, cb.PrimeMultisigAddr, primeCluster.Config.NetworkMagic, bridgingRequestMetadata))

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
		Address string `cbor:"address" json:"address"`
		Amount  uint64 `cbor:"amount" json:"amount"`
	}

	var transactions = make([]BridgingRequestMetadataTransaction, 0, len(receivers))
	for addr, amount := range receivers {
		transactions = append(transactions, BridgingRequestMetadataTransaction{
			Address: addr,
			Amount:  amount,
		})
	}

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"type":               "bridgingRequest",
			"destinationChainId": "vector",
			"senderAddr":         sender,
			"transactions":       transactions,
		},
	}

	return json.Marshal(metadata)
}
