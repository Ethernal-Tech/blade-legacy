package cardanofw

import (
	"fmt"
	"io"
	"testing"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
)

type TestOgmiosServerConfig struct {
	ConfigFile string
	Binary     string
	Port       int
	SocketPath string
	StdOut     io.Writer
}

type TestOgmiosServer struct {
	t *testing.T

	config *TestOgmiosServerConfig
	node   *framework.Node
}

func NewOgmiosTestServer(t *testing.T, config *TestOgmiosServerConfig) (*TestOgmiosServer, error) {
	t.Helper()

	if config.Binary == "" {
		config.Binary = resolveOgmiosBinary()
	}

	srv := &TestOgmiosServer{
		t:      t,
		config: config,
	}

	return srv, srv.Start()
}

func (t *TestOgmiosServer) IsRunning() bool {
	return t.node != nil
}

func (t *TestOgmiosServer) Stop() error {
	if err := t.node.Stop(); err != nil {
		return err
	}

	t.node = nil

	return nil
}

func (t *TestOgmiosServer) Start() error {
	// Build arguments
	args := []string{
		"--port", fmt.Sprint(t.config.Port),
		"--node-socket", t.config.SocketPath,
		"--node-config", t.config.ConfigFile,
	}

	node, err := framework.NewNode(t.config.Binary, args, t.config.StdOut)
	if err != nil {
		return err
	}

	t.node = node

	return nil
}

func (t TestOgmiosServer) SocketPath() string {
	return t.config.SocketPath
}

func (t TestOgmiosServer) Port() int {
	return t.config.Port
}

func (t TestOgmiosServer) URL() string {
	return fmt.Sprintf("localhost:%d", t.config.Port)
}
