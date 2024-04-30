package cardanofw

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/helper/common"
	cardano_wallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

//go:embed files/*
var cardanoFiles embed.FS

const hostIP = "cluster-%d-node-%d"

func resolveCardanoCliBinary() string {
	bin := os.Getenv("CARDANO_CLI_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return "cardano-cli"
}

type TestCardanoClusterConfig struct {
	t *testing.T

	ID             int
	NetworkMagic   int
	SecurityParam  int
	NodesCount     int
	StartNodeID    int
	Port           int
	InitialSupply  *big.Int
	BlockTimeMilis int
	StartTimeDelay time.Duration

	WithLogs   bool
	WithStdout bool
	LogsDir    string
	TmpDir     string
	Binary     string

	logsDirOnce sync.Once
}

func (c *TestCardanoClusterConfig) Dir(name string) string {
	return filepath.Join(c.TmpDir, name)
}

func (c *TestCardanoClusterConfig) GetStdout(name string, custom ...io.Writer) io.Writer {
	writers := []io.Writer{}

	if c.WithLogs {
		c.logsDirOnce.Do(func() {
			if err := c.initLogsDir(); err != nil {
				c.t.Fatal("GetStdout init logs dir", "err", err)
			}
		})

		f, err := os.OpenFile(filepath.Join(c.LogsDir, name+".log"), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			c.t.Log("GetStdout open file error", "err", err)
		} else {
			writers = append(writers, f)

			c.t.Cleanup(func() {
				if err := f.Close(); err != nil {
					c.t.Log("GetStdout close file error", "err", err)
				}
			})
		}
	}

	if c.WithStdout {
		writers = append(writers, os.Stdout)
	}

	if len(custom) > 0 {
		writers = append(writers, custom...)
	}

	if len(writers) == 0 {
		return io.Discard
	}

	return io.MultiWriter(writers...)
}

func (c *TestCardanoClusterConfig) initLogsDir() error {
	if c.LogsDir == "" {
		logsDir := path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().UTC().Unix()), c.t.Name())
		if err := common.CreateDirSafe(logsDir, 0750); err != nil {
			return err
		}

		c.t.Logf("logs enabled for e2e test: %s", logsDir)
		c.LogsDir = logsDir
	}

	return nil
}

type TestCardanoCluster struct {
	Config  *TestCardanoClusterConfig
	Servers []*TestCardanoServer

	once         sync.Once
	failCh       chan struct{}
	executionErr error
}

type CardanoClusterOption func(*TestCardanoClusterConfig)

func WithNodesCount(num int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.NodesCount = num
	}
}

func WithBlockTime(blockTimeMilis int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.BlockTimeMilis = blockTimeMilis
	}
}

func WithStartTimeDelay(delay time.Duration) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.StartTimeDelay = delay
	}
}

func WithStartNodeID(startNodeID int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.StartNodeID = startNodeID
	}
}

func WithPort(port int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.Port = port
	}
}

func WithLogsDir(logsDir string) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.LogsDir = logsDir
	}
}

func WithNetworkMagic(networkMagic int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.NetworkMagic = networkMagic
	}
}

func WithID(id int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.ID = id
	}
}

func NewCardanoTestCluster(t *testing.T, opts ...CardanoClusterOption) (*TestCardanoCluster, error) {
	t.Helper()
	// var err error

	config := &TestCardanoClusterConfig{
		t:          t,
		WithLogs:   true, // strings.ToLower(os.Getenv(e)) == "true"
		WithStdout: true, // strings.ToLower(os.Getenv(envStdoutEnabled)) == "true"
		Binary:     resolveCardanoCliBinary(),

		NetworkMagic:   42,
		SecurityParam:  10,
		NodesCount:     3,
		InitialSupply:  new(big.Int).SetUint64(12000000),
		StartTimeDelay: time.Second * 30,
		BlockTimeMilis: 2000,
		Port:           3000,
	}

	startTime := time.Now().UTC().Add(config.StartTimeDelay)

	for _, opt := range opts {
		opt(config)
	}

	clusterName := fmt.Sprintf("cluster-%d", config.ID)
	config.TmpDir = path.Join("../../e2e-docker-tmp", clusterName)

	if common.DirectoryExists(path.Dir(config.TmpDir)) {
		if err := os.RemoveAll(path.Dir(config.TmpDir)); err != nil {
			return nil, err
		}
	}

	if err := common.CreateDirSafe(config.TmpDir, 0750); err != nil {
		return nil, err
	}

	cluster := &TestCardanoCluster{
		Servers: []*TestCardanoServer{},
		Config:  config,
		failCh:  make(chan struct{}),
		once:    sync.Once{},
	}

	// init genesis
	if err := cluster.InitGenesis(startTime.Unix()); err != nil {
		return nil, err
	}

	// copy config files
	if err := cluster.CopyConfigFilesStep1(); err != nil {
		return nil, err
	}

	// genesis create staked - babbage
	if err := cluster.GenesisCreateStaked(startTime); err != nil {
		return nil, err
	}

	// final step before starting nodes
	if err := cluster.CopyConfigFilesAndInitDirectoriesStep2(); err != nil {
		return nil, err
	}

	for i := 1; i <= cluster.Config.NodesCount; i++ {
		id := cluster.Config.StartNodeID + i
		port := cluster.Config.Port + i
		socketPath := fmt.Sprintf("node-spo%d/node.socket", id)

		server, err := NewCardanoTestServer(id, port, uint(config.NetworkMagic), config.Dir(""), socketPath)
		if err != nil {
			return nil, err
		}

		cluster.Servers = append(cluster.Servers, server)
	}

	if err := cluster.GenerateDockerComposeFiles(); err != nil {
		return nil, err
	}

	return cluster, nil
}

func (c *TestCardanoCluster) StartDocker() error {
	dockerFile := filepath.Join(c.Config.TmpDir, "docker-compose.yml")

	var b bytes.Buffer
	stdOut := c.Config.GetStdout("docker-compose", &b)

	err := c.runCommand("docker-compose", []string{"-f", dockerFile, "up", "-d"}, stdOut)
	if err != nil {
		cntErrors := strings.Count(err.Error(), "error")
		if cntErrors == 1 &&
			strings.Contains(err.Error(), "error during command execution: Creating network") {
			err = nil
		}
	}

	return err
}

func (c *TestCardanoCluster) StopDocker() error {
	dockerFile := filepath.Join(c.Config.TmpDir, "docker-compose.yml")

	var b bytes.Buffer
	stdOut := c.Config.GetStdout("docker-compose", &b)

	return c.runCommand("docker-compose", []string{"-f", dockerFile, "down"}, stdOut)
}

func (c *TestCardanoCluster) Stats(
	ctx context.Context,
) ([]cardano_wallet.QueryTipData, bool, error) {
	result := make([]cardano_wallet.QueryTipData, len(c.Servers))
	errs := make([]error, len(c.Servers))
	ready := uint32(1)
	wg := sync.WaitGroup{}

	for i, srv := range c.Servers {
		wg.Add(1)

		go func(indx int, txProv cardano_wallet.ITxProvider) {
			defer wg.Done()

			tip, err := txProv.GetTip(ctx)
			if err != nil {
				c.Config.t.Log("query tip error", "indx", indx, "err", err)

				if strings.Contains(err.Error(), "cardano-cli: Network.Socket.connect") &&
					strings.Contains(err.Error(), "does not exist") {
					atomic.StoreUint32(&ready, 0)
				} else {
					errs[indx] = fmt.Errorf("error for provider %d: %w", indx, err)
				}

				return
			}

			result[indx] = tip
		}(i, srv.GetTxProvider())
	}

	wg.Wait()

	return result, ready == uint32(1), errors.Join(errs...)
}

func (c *TestCardanoCluster) WaitUntil(timeout, frequency time.Duration, handler func() (bool, error)) error {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return fmt.Errorf("timeout")
		case <-c.failCh:
			return c.executionErr
		case <-ticker.C:
		}

		finish, err := handler()
		if err != nil {
			return err
		} else if finish {
			return nil
		}
	}
}

func (c *TestCardanoCluster) WaitForReady(ctx context.Context, timeout time.Duration) error {
	return c.WaitUntil(timeout, time.Second*2, func() (bool, error) {
		_, ready, err := c.Stats(ctx)

		return ready, err
	})
}

func (c *TestCardanoCluster) WaitForBlock(
	ctx context.Context, n uint64, timeout time.Duration, frequency time.Duration,
) error {
	return c.WaitUntil(timeout, frequency, func() (bool, error) {
		tips, ready, err := c.Stats(ctx)
		if err != nil {
			return false, err
		} else if !ready {
			return false, nil
		}

		c.Config.t.Log("WaitForBlock", "tips", tips)

		for _, tip := range tips {
			if tip.Block < n {
				return false, nil
			}
		}

		return true, nil
	})
}

func (c *TestCardanoCluster) WaitForBlockWithState(
	ctx context.Context, n uint64, timeout time.Duration,
) error {
	servers := c.Servers
	blockState := make(map[uint64]map[int]string, len(c.Servers))

	return c.WaitUntil(timeout, time.Millisecond*200, func() (bool, error) {
		tips, ready, err := c.Stats(ctx)
		if err != nil {
			return false, err
		} else if !ready {
			return false, nil
		}

		c.Config.t.Log("WaitForBlockWithState", "tips", tips)

		for i, bn := range tips {
			serverID := servers[i].ID()
			// bn == nil -> server is stopped + dont remember smaller than n blocks
			if bn.Block < n {
				continue
			}

			if mp, exists := blockState[bn.Block]; exists {
				mp[serverID] = bn.Hash
			} else {
				blockState[bn.Block] = map[int]string{
					serverID: bn.Hash,
				}
			}
		}

		// for all running servers there must be at least one block >= n
		// that all servers have with same hash
		for _, mp := range blockState {
			if len(mp) != len(c.Servers) {
				continue
			}

			hash, ok := "", true

			for _, h := range mp {
				if hash == "" {
					hash = h
				} else if h != hash {
					ok = false

					break
				}
			}

			if ok {
				return true, nil
			}
		}

		return false, nil
	})
}

// runCommand executes command with given arguments
func (c *TestCardanoCluster) runCommand(binary string, args []string, stdout io.Writer, envVariables ...string) error {
	var stdErr bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Stderr = &stdErr
	cmd.Stdout = stdout

	cmd.Env = append(os.Environ(), envVariables...)
	// fmt.Printf("$ %s %s\n", binary, strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		if stdErr.Len() > 0 {
			return fmt.Errorf("failed to execute command: %s", stdErr.String())
		}

		return fmt.Errorf("failed to execute command: %w", err)
	}

	if stdErr.Len() > 0 {
		return fmt.Errorf("error during command execution: %s", stdErr.String())
	}

	return nil
}

func (c *TestCardanoCluster) InitGenesis(startTime int64) error {
	var b bytes.Buffer

	fnContent, err := cardanoFiles.ReadFile("files/byron.genesis.spec.json")
	if err != nil {
		return err
	}

	fnContent, err = updateJSON(fnContent, func(mp map[string]interface{}) {
		mp["slotDuration"] = strconv.Itoa(c.Config.BlockTimeMilis)
	})
	if err != nil {
		return err
	}

	protParamsFile := c.Config.Dir("byron.genesis.spec.json")
	if err := os.WriteFile(protParamsFile, fnContent, 0600); err != nil {
		return err
	}

	args := []string{
		"byron", "genesis", "genesis",
		"--protocol-magic", strconv.Itoa(c.Config.NetworkMagic),
		"--start-time", strconv.FormatInt(startTime, 10),
		"--k", strconv.Itoa(c.Config.SecurityParam),
		"--n-poor-addresses", "0",
		"--n-delegate-addresses", strconv.Itoa(c.Config.NodesCount),
		"--total-balance", c.Config.InitialSupply.String(),
		"--delegate-share", "1",
		"--avvm-entry-count", "0",
		"--avvm-entry-balance", "0",
		"--protocol-parameters-file", protParamsFile,
		"--genesis-output-dir", c.Config.Dir("byron-gen-command"),
	}
	stdOut := c.Config.GetStdout("cardano-genesis", &b)

	return c.runCommand(c.Config.Binary, args, stdOut)
}

func (c *TestCardanoCluster) CopyConfigFilesStep1() error {
	items := [][2]string{
		{"alonzo-babbage-test-genesis.json", "genesis.alonzo.spec.json"},
		{"conway-babbage-test-genesis.json", "genesis.conway.spec.json"},
		{"configuration.yaml", "configuration.yaml"},
	}
	for _, it := range items {
		fnContent, err := cardanoFiles.ReadFile("files/" + it[0])
		if err != nil {
			return err
		}

		protParamsFile := c.Config.Dir(it[1])
		if err := os.WriteFile(protParamsFile, fnContent, 0600); err != nil {
			return err
		}
	}

	return nil
}

func (c *TestCardanoCluster) CopyConfigFilesAndInitDirectoriesStep2() error {
	if err := common.CreateDirSafe(c.Config.Dir("genesis/byron"), 0750); err != nil {
		return err
	}

	if err := common.CreateDirSafe(c.Config.Dir("genesis/shelley"), 0750); err != nil {
		return err
	}

	err := updateJSONFile(
		c.Config.Dir("byron-gen-command/genesis.json"),
		c.Config.Dir("genesis/byron/genesis.json"),
		func(mp map[string]interface{}) {
			// mp["protocolConsts"].(map[string]interface{})["protocolMagic"] = 42
		})
	if err != nil {
		return err
	}

	err = updateJSONFile(
		c.Config.Dir("genesis.json"),
		c.Config.Dir("genesis/shelley/genesis.json"),
		func(mp map[string]interface{}) {
			mp["slotLength"] = 0.1
			mp["activeSlotsCoeff"] = 0.1
			mp["securityParam"] = 10
			mp["epochLength"] = 500
			mp["maxLovelaceSupply"] = 1000000000000
			mp["updateQuorum"] = 2
			prParams := getMapFromInterfaceKey(mp, "protocolParams")
			getMapFromInterfaceKey(prParams, "protocolVersion")["major"] = 7
			prParams["minFeeA"] = 44
			prParams["minFeeB"] = 155381
			prParams["minUTxOValue"] = 1000000
			prParams["decentralisationParam"] = 0.7
			prParams["rho"] = 0.1
			prParams["tau"] = 0.1
		})
	if err != nil {
		return err
	}

	if err := os.Rename(
		c.Config.Dir("genesis.alonzo.json"),
		c.Config.Dir("genesis/shelley/genesis.alonzo.json"),
	); err != nil {
		return err
	}

	if err := os.Rename(
		c.Config.Dir("genesis.conway.json"),
		c.Config.Dir("genesis/shelley/genesis.conway.json"),
	); err != nil {
		return err
	}

	for i := 0; i < c.Config.NodesCount; i++ {
		nodeID := i + 1
		if err := common.CreateDirSafe(c.Config.Dir(fmt.Sprintf("node-spo%d", nodeID)), 0750); err != nil {
			return err
		}

		producers := make([]map[string]interface{}, 0, c.Config.NodesCount-1)

		for pid := 0; pid < c.Config.NodesCount; pid++ {
			if i != pid {
				producers = append(producers, map[string]interface{}{
					"addr":    fmt.Sprintf(hostIP, c.Config.ID, pid),
					"valency": 1,
					"port":    c.Config.Port + pid,
				})
			}
		}

		topologyJSONContent, err := json.MarshalIndent(map[string]interface{}{
			"Producers": producers,
		}, "", "    ")
		if err != nil {
			return err
		}

		if err := os.WriteFile(
			c.Config.Dir(fmt.Sprintf("node-spo%d/topology.json", nodeID)),
			topologyJSONContent,
			0600,
		); err != nil {
			return err
		}

		// keys
		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("pools/vrf%d.skey", nodeID)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/vrf.skey", nodeID))); err != nil {
			return err
		}

		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("pools/opcert%d.cert", nodeID)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/opcert.cert", nodeID))); err != nil {
			return err
		}

		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("pools/kes%d.skey", nodeID)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/kes.skey", nodeID))); err != nil {
			return err
		}

		// byron related
		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("byron-gen-command/delegate-keys.%03d.key", i)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/byron-delegate.key", nodeID))); err != nil {
			return err
		}

		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("byron-gen-command/delegation-cert.%03d.json", i)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/byron-delegation.cert", nodeID))); err != nil {
			return err
		}
	}

	return nil
}

func (c *TestCardanoCluster) GenerateDockerComposeFiles() error {
	filePath := c.Config.Dir("docker-compose.yml")

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	_, _ = writer.WriteString("version: \"3.9\"\n\n")
	_, _ = writer.WriteString("services:\n")

	for _, srv := range c.Servers {
		nodeID := srv.ID()
		dockerSocketPath := srv.SocketPath("/node-data")

		_, _ = writer.WriteString(fmt.Sprintf("  cluster-%d-node-%d:\n", c.Config.ID, nodeID))
		_, _ = writer.WriteString("    image: ghcr.io/intersectmbo/cardano-node:8.7.3\n")
		_, _ = writer.WriteString("    environment:\n")
		_, _ = writer.WriteString("      - CARDANO_BLOCK_PRODUCER=true\n")
		_, _ = writer.WriteString("      - CARDANO_CONFIG=/node-data/configuration.yaml\n")
		_, _ = writer.WriteString(fmt.Sprintf("      - CARDANO_TOPOLOGY=/node-data/node-spo%d/topology.json\n", nodeID))
		_, _ = writer.WriteString(fmt.Sprintf("      - CARDANO_DATABASE_PATH=/node-data/node-spo%d/db\n", nodeID))
		_, _ = writer.WriteString(fmt.Sprintf("      - CARDANO_SOCKET_PATH=%s\n", dockerSocketPath))
		_, _ = writer.WriteString(fmt.Sprintf("      - CARDANO_SHELLEY_KES_KEY=/node-data/node-spo%d/kes.skey\n", nodeID))
		_, _ = writer.WriteString(fmt.Sprintf("      - CARDANO_SHELLEY_VRF_KEY=/node-data/node-spo%d/vrf.skey\n", nodeID))
		_, _ = writer.WriteString(
			fmt.Sprintf("      - CARDANO_SHELLEY_OPERATIONAL_CERTIFICATE=/node-data/node-spo%d/opcert.cert\n", nodeID))
		_, _ = writer.WriteString(fmt.Sprintf("      - CARDANO_LOG_DIR=/node-data/node-spo%d/node.log\n", nodeID))
		_, _ = writer.WriteString("      - CARDANO_BIND_ADDR=0.0.0.0\n")
		_, _ = writer.WriteString(fmt.Sprintf("      - CARDANO_PORT=%d\n", srv.Port()))
		_, _ = writer.WriteString("    command:\n")
		_, _ = writer.WriteString("      - run\n")
		_, _ = writer.WriteString("    ports:\n")
		_, _ = writer.WriteString(fmt.Sprintf("      - %d:%d\n", srv.Port(), srv.Port()))
		_, _ = writer.WriteString("    volumes:\n")
		_, _ = writer.WriteString(fmt.Sprintf("      - %s:/node-data\n", c.Config.Dir("")))
		_, _ = writer.WriteString("    restart: on-failure\n")
		_, _ = writer.WriteString("    logging:\n")
		_, _ = writer.WriteString("      driver: \"json-file\"\n")
		_, _ = writer.WriteString("      options:\n")
		_, _ = writer.WriteString("        max-size: \"200k\"\n")
		_, _ = writer.WriteString("        max-file: \"10\"\n")
		_, _ = writer.WriteString("\n")
	}

	err = writer.Flush()
	if err != nil {
		return err
	}

	return nil
}

// Because in Babbage the overlay schedule and decentralization parameter are deprecated,
// we must use the "create-staked" cli command to create SPOs in the ShelleyGenesis
func (c *TestCardanoCluster) GenesisCreateStaked(startTime time.Time) error {
	var b bytes.Buffer

	exprectedErr := fmt.Sprintf(
		"%d genesis keys, %d non-delegating UTxO keys, %d stake pools, %d delegating UTxO keys, %d delegation map entries",
		c.Config.NodesCount, c.Config.NodesCount, c.Config.NodesCount, c.Config.NodesCount, c.Config.NodesCount)
	args := []string{
		"genesis", "create-staked",
		"--genesis-dir", c.Config.Dir(""),
		"--testnet-magic", strconv.Itoa(c.Config.NetworkMagic),
		"--start-time", startTime.Format("2006-01-02T15:04:05Z"),
		"--supply", "2000000000000",
		"--supply-delegated", "240000000002",
		"--gen-genesis-keys", strconv.Itoa(c.Config.NodesCount),
		"--gen-pools", strconv.Itoa(c.Config.NodesCount),
		"--gen-stake-delegs", strconv.Itoa(c.Config.NodesCount),
		"--gen-utxo-keys", strconv.Itoa(c.Config.NodesCount),
	}
	stdOut := c.Config.GetStdout("cardano-genesis-create-staked", &b)

	err := c.runCommand(c.Config.Binary, args, stdOut)
	if strings.Contains(err.Error(), exprectedErr) {
		return nil
	}

	return err
}

func updateJSON(content []byte, callback func(mp map[string]interface{})) ([]byte, error) {
	// Parse []byte into a map
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, err
	}

	callback(data)

	return json.MarshalIndent(data, "", "    ") // The second argument is the prefix, and the third is the indentation
}

func updateJSONFile(fn1 string, fn2 string, callback func(mp map[string]interface{})) error {
	bytes, err := os.ReadFile(fn1)
	if err != nil {
		return err
	}

	bytes, err = updateJSON(bytes, callback)
	if err != nil {
		return err
	}

	return os.WriteFile(fn2, bytes, 0600)
}

func getMapFromInterfaceKey(mp map[string]interface{}, key string) map[string]interface{} {
	var prParams map[string]interface{}

	if v, exists := mp[key]; !exists {
		prParams = map[string]interface{}{}
		mp[key] = prParams
	} else {
		prParams, _ = v.(map[string]interface{})
	}

	return prParams
}
