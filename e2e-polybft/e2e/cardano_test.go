package e2e

import (
	"fmt"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/blockfrost"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/0xPolygon/polygon-edge/helper/common"
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
		baseLogsDir string = path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().Unix()), t.Name())
	)

	go blockfrost.ResetDbSync(15)

	for i := 0; i < clusterCnt; i++ {
		id := i
		wg.Add(1)

		go func() {
			defer wg.Done()

			logsDir := fmt.Sprintf("%s/%d", baseLogsDir, id)
			if err := common.CreateDirSafe(logsDir, 0750); err != nil {
				errors[id] = err

				return
			}

			cluster, err := cardanofw.NewCardanoTestCluster(t,
				cardanofw.WithId(id+1),
				cardanofw.WithNodesCount(4),
				cardanofw.WithStartTimeDelay(time.Second*5),
				cardanofw.WithPort(5000+id*100),
				cardanofw.WithLogsDir(logsDir),
				cardanofw.WithNetworkMagic(42+id))
			if err != nil {
				errors[id] = err

				return
			}

			defer cluster.StopDocker()

			t.Log("Waiting for sockets to be ready", "id", id+1, "sockets", strings.Join(cluster.GetSockets(), ", "))
			if errors[id] = cluster.WaitForReady(time.Second * 100); errors[id] != nil {
				return
			}

			t.Log("Waiting for blocks", "id", id+1)

			t.Log("starting blockfrost")
			bf, err := blockfrost.NewBlockFrost(cluster, id+1)
			if err != nil {
				errors[id] = err
				return
			}

			if errors[id] = bf.Start(); errors[id] != nil {
				return
			}

			defer bf.Stop()

			errors[id] = cluster.WaitForBlockWithState(10, time.Second*300)
		}()
	}

	wg.Wait()

	for i := 0; i < clusterCnt; i++ {
		assert.NoError(t, errors[i])
	}
}
