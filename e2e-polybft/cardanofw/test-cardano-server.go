package cardanofw

import (
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
)

type TestCardanoServerConfig struct {
	ID         int
	NodeDir    string
	ConfigFile string
	Binary     string
	Port       int
	StdOut     io.Writer
}

type TestCardanoServer struct {
	t *testing.T

	config *TestCardanoServerConfig
	node   *framework.Node
}

func NewCardanoTestServer(t *testing.T, config *TestCardanoServerConfig) (*TestCardanoServer, error) {
	if config.Binary == "" {
		config.Binary = resolveCardanoNodeBinary()
	}

	srv := &TestCardanoServer{
		t:      t,
		config: config,
	}

	return srv, srv.Start()
}

func (t *TestCardanoServer) IsRunning() bool {
	return t.node != nil
}

func (t *TestCardanoServer) Stop() error {
	if err := t.node.Stop(); err != nil {
		return err
	}

	t.node = nil

	return nil
}

func (t *TestCardanoServer) Start() error {
	// Build arguments
	args := []string{
		"run",
		"--config", t.config.ConfigFile,
		"--topology", fmt.Sprintf("%s/topology.json", t.config.NodeDir),
		"--database-path", fmt.Sprintf("%s/db", t.config.NodeDir),
		"--socket-path", t.SocketPath(),
		"--shelley-kes-key", fmt.Sprintf("%s/kes.skey", t.config.NodeDir),
		"--shelley-vrf-key", fmt.Sprintf("%s/vrf.skey", t.config.NodeDir),
		"--byron-delegation-certificate", fmt.Sprintf("%s/byron-delegation.cert", t.config.NodeDir),
		"--byron-signing-key", fmt.Sprintf("%s/byron-delegate.key", t.config.NodeDir),
		"--shelley-operational-certificate", fmt.Sprintf("%s/opcert.cert", t.config.NodeDir),
		"--port", strconv.Itoa(t.config.Port),
	}

	node, err := framework.NewNode(t.config.Binary, args, t.config.StdOut)
	if err != nil {
		return err
	}

	t.node = node

	return nil
}

func (t TestCardanoServer) ID() int {
	return t.config.ID
}

func (t TestCardanoServer) SocketPath() string {
	// socketPath handle for windows \\.\pipe\
	return fmt.Sprintf("%s/node.sock", t.config.NodeDir)
}

func (t TestCardanoServer) Port() int {
	return t.config.Port
}
