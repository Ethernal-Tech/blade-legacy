package cardanofw

import (
	"bytes"
	"embed"
	"encoding/json"
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
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/helper/common"
)

//go:embed files/*
var cardanoFiles embed.FS

const hostIP = "127.0.0.1"

func resolveCardanoNodeBinary() string {
	bin := os.Getenv("CARDANO_NODE_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return "cardano-node"
}

func resolveCardanoCliBinary() string {
	bin := os.Getenv("CARDANO_CLI_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return "cardano-cli"
}

func resolveOgmiosBinary() string {
	bin := os.Getenv("OGMIOS")
	if bin != "" {
		return bin
	}
	// fallback
	return "ogmios"
}

type TestCardanoClusterConfig struct {
	t *testing.T

	ID             int
	NetworkMagic   int
	SecurityParam  int
	NodesCount     int
	StartNodeID    int
	Port           int
	OgmiosPort     int
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
	Config       *TestCardanoClusterConfig
	Servers      []*TestCardanoServer
	OgmiosServer *TestOgmiosServer

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

func WithStartNodeID(startNodeID int) CardanoClusterOption { // COM: Should this be removed?
	return func(h *TestCardanoClusterConfig) {
		h.StartNodeID = startNodeID
	}
}

func WithPort(port int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.Port = port
	}
}

func WithOgmiosPort(ogmiosPort int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.OgmiosPort = ogmiosPort
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

	var err error

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
		OgmiosPort:     1337,
	}

	startTime := time.Now().UTC().Add(config.StartTimeDelay)

	for _, opt := range opts {
		opt(config)
	}

	config.TmpDir, err = os.MkdirTemp("/tmp", "cardano-")
	if err != nil {
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

	for i := 0; i < cluster.Config.NodesCount; i++ {
		// time.Sleep(time.Second * 5)
		err = cluster.NewTestServer(t, i+1, config.Port+i)
		if err != nil {
			return nil, err
		}
	}

	return cluster, nil
}

func (c *TestCardanoCluster) NewTestServer(t *testing.T, id int, port int) error {
	t.Helper()

	srv, err := NewCardanoTestServer(t, &TestCardanoServerConfig{
		ID:   id,
		Port: port,
		//StdOut:       c.Config.GetStdout(fmt.Sprintf("node-%d", id)),
		ConfigFile:   c.Config.Dir("configuration.yaml"),
		NodeDir:      c.Config.Dir(fmt.Sprintf("node-spo%d", id)),
		Binary:       resolveCardanoNodeBinary(),
		NetworkMagic: c.Config.NetworkMagic,
	})
	if err != nil {
		return err
	}

	// watch the server for stop signals. It is important to fix the specific
	// 'node' reference since 'TestServer' creates a new one if restarted.
	go func(node *framework.Node) {
		<-node.Wait()

		if !node.ExitResult().Signaled {
			c.Fail(fmt.Errorf("server id = %d, port = %d has stopped unexpectedly", id, port))
		}
	}(srv.node)

	c.Servers = append(c.Servers, srv)

	return err
}

func (c *TestCardanoCluster) Fail(err error) {
	c.once.Do(func() {
		c.executionErr = err
		close(c.failCh)
	})
}

func (c *TestCardanoCluster) Stop() error {
	for _, srv := range c.Servers {
		if srv.IsRunning() {
			if err := srv.Stop(); err != nil {
				return err
			}
		}
	}

	if c.OgmiosServer.IsRunning() {
		if err := c.OgmiosServer.Stop(); err != nil {
			return err
		}
	}

	return nil
}

func (c *TestCardanoCluster) OgmiosURL() string {
	return fmt.Sprintf("http://localhost:%d", c.Config.OgmiosPort)
}

func (c *TestCardanoCluster) NetworkURL() string {
	return fmt.Sprintf("http://localhost:%d", c.Config.Port)
}

func (c *TestCardanoCluster) Stats() ([]*TestCardanoStats, bool, error) {
	blocks := make([]*TestCardanoStats, len(c.Servers))
	ready := make([]bool, len(c.Servers))
	errors := make([]error, len(c.Servers))
	wg := sync.WaitGroup{}

	for i := range c.Servers {
		id, srv := i, c.Servers[i]
		if !srv.IsRunning() {
			ready[id] = true

			continue
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			var b bytes.Buffer

			stdOut := c.Config.GetStdout(fmt.Sprintf("cardano-stats-%d", srv.ID()), &b)
			args := []string{
				"query", "tip",
				"--testnet-magic", strconv.Itoa(c.Config.NetworkMagic),
				"--socket-path", srv.SocketPath(),
			}

			if err := c.runCommand(c.Config.Binary, args, stdOut); err != nil {
				if strings.Contains(err.Error(), "Network.Socket.connect") &&
					strings.Contains(err.Error(), "does not exist (No such file or directory)") {
					c.Config.t.Log("socket error", "path", srv.SocketPath(), "err", err)

					return
				}

				ready[id], errors[id] = true, err

				return
			}

			stat, err := NewTestCardanoStats(b.Bytes())
			if err != nil {
				ready[id], errors[id] = true, err
			}

			ready[id], blocks[id] = true, stat
		}()
	}

	wg.Wait()

	for i, err := range errors {
		if err != nil {
			return nil, true, err
		} else if !ready[i] {
			return nil, false, nil
		}
	}

	return blocks, true, nil
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

func (c *TestCardanoCluster) WaitForReady(timeout time.Duration) error {
	return c.WaitUntil(timeout, time.Second*2, func() (bool, error) {
		_, ready, err := c.Stats()

		return ready, err
	})
}

func (c *TestCardanoCluster) WaitForBlock(
	n uint64, timeout time.Duration, frequency time.Duration,
) error {
	return c.WaitUntil(timeout, frequency, func() (bool, error) {
		tips, ready, err := c.Stats()
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
	n uint64, timeout time.Duration,
) error {
	servers := c.Servers
	blockState := make(map[uint64]map[int]string, len(c.Servers))

	return c.WaitUntil(timeout, time.Millisecond*200, func() (bool, error) {
		tips, ready, err := c.Stats()
		if err != nil {
			return false, err
		} else if !ready {
			return false, nil
		}

		fmt.Print("WaitForBlockWithState", "tips", tips)

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

func (c *TestCardanoCluster) StartOgmios(t *testing.T) error {
	t.Helper()

	srv, err := NewOgmiosTestServer(t, &TestOgmiosServerConfig{
		ConfigFile: c.Servers[0].config.ConfigFile,
		Binary:     resolveOgmiosBinary(),
		Port:       c.Config.OgmiosPort,
		SocketPath: c.Servers[0].SocketPath(),
		StdOut:     c.Config.GetStdout(fmt.Sprintf("ogmios-%d", c.Config.ID)),
	})
	if err != nil {
		return err
	}

	// watch the server for stop signals. It is important to fix the specific
	// 'node' reference since 'TestServer' creates a new one if restarted.
	go func(node *framework.Node, id int, port int) {
		<-node.Wait()

		if !node.ExitResult().Signaled {
			c.Fail(fmt.Errorf("ogmios id = %d, port = %d has stopped unexpectedly", id, port))
		}
	}(srv.node, c.Config.ID, c.Config.OgmiosPort)

	c.OgmiosServer = srv

	return err
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
					"addr":    hostIP,
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

func (c *TestCardanoCluster) RunningServersCount() int {
	cnt := 0

	for _, srv := range c.Servers {
		if srv.IsRunning() {
			cnt++
		}
	}

	return cnt
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
