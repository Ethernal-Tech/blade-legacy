package cardanofw

import (
	"fmt"
	"path"

	cardano_wallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

type TestCardanoServer struct {
	id         int
	port       int
	socketPath string
	txProvider cardano_wallet.ITxProvider
}

func NewCardanoTestServer(
	id int, port int, networkMagic uint, socketPathPrefix string, socketPath string,
) (*TestCardanoServer, error) {
	fullPath := path.Join(socketPathPrefix, socketPath)

	txProvider, err := cardano_wallet.NewTxProviderCli(networkMagic, fullPath)
	if err != nil {
		return nil, err
	}

	return &TestCardanoServer{
		id:         id,
		port:       port,
		socketPath: socketPath,
		txProvider: txProvider,
	}, nil
}

func (t TestCardanoServer) ID() int {
	return t.id
}

func (t TestCardanoServer) SocketPath(prefix string) string {
	// socketPath handle for windows \\.\pipe\
	return path.Join(prefix, t.socketPath)
}

func (t TestCardanoServer) Port() int {
	return t.port
}

func (t TestCardanoServer) URL() string {
	return fmt.Sprintf("localhost:%d", t.port)
}

func (t TestCardanoServer) GetTxProvider() cardano_wallet.ITxProvider {
	return t.txProvider
}
