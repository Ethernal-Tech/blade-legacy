package e2e

import (
	"context"
	"fmt"
	"math/big"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/blockfrost"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/assert"
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

			errors[id] = blockfrost.WaitUntilBlock(t, context.Background(), txProvider, 4, time.Second*120)
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

				err = cardanofw.PopulateAddress(ctx, txProvider, cluster.Config.TmpDir, sendAmount, receiver, uint(cluster.Config.NetworkMagic), 1, clusterCnt)
				if checkAndSetError(err) {
					return
				}

				cmpHandler := func(val *big.Int) bool {
					return val.Cmp(new(big.Int).SetUint64(sendAmount)) >= 0
				}

				err = wallet.WaitForAmount(context.Background(), txProvider, receiver, cmpHandler, 10, time.Second*60)
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
