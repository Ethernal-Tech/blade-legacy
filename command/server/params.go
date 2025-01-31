package server

import (
	"errors"
	"net"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/command/server/config"
	"github.com/0xPolygon/polygon-edge/network"
	"github.com/0xPolygon/polygon-edge/secrets"
	"github.com/0xPolygon/polygon-edge/server"
	"github.com/hashicorp/go-hclog"
	"github.com/multiformats/go-multiaddr"
)

const (
	configFlag                   = "config"
	genesisPathFlag              = "chain"
	dataDirFlag                  = "data-dir"
	libp2pAddressFlag            = "libp2p"
	prometheusAddressFlag        = "prometheus"
	natFlag                      = "nat"
	dnsFlag                      = "dns"
	sealFlag                     = "seal"
	maxPeersFlag                 = "max-peers"
	maxInboundPeersFlag          = "max-inbound-peers"
	maxOutboundPeersFlag         = "max-outbound-peers"
	priceLimitFlag               = "price-limit"
	jsonRPCBatchRequestLimitFlag = "json-rpc-batch-request-limit"
	jsonRPCBlockRangeLimitFlag   = "json-rpc-block-range-limit"
	maxSlotsFlag                 = "max-slots"
	maxEnqueuedFlag              = "max-enqueued"
	blockGasTargetFlag           = "block-gas-target"
	secretsConfigFlag            = "secrets-config"
	restoreFlag                  = "restore"
	devIntervalFlag              = "dev-interval"
	devFlag                      = "dev"
	corsOriginFlag               = "access-control-allow-origins"
	logFileLocationFlag          = "log-to"
	useTLSFlag                   = "use-tls"
	tlsCertFileLocationFlag      = "tls-cert-file"
	tlsKeyFileLocationFlag       = "tls-key-file"
	gossipMessageSizeFlag        = "gossip-msg-size"
	txGossipBatchSizeFlag        = "tx-gossip-batch-size"

	relayerFlag = "relayer"

	concurrentRequestsDebugFlag = "concurrent-requests-debug"
	webSocketReadLimitFlag      = "websocket-read-limit"

	metricsIntervalFlag = "metrics-interval"

	// event tracker
	trackerSyncBatchSizeFlag          = "sync-batch-size"
	trackerNumBlockConfirmationsFlag  = "num-block-confirmations"
	trackerNumOfBlocksToReconcileFlag = "num-blocks-reconcile"
)

const (
	unsetPeersValue = -1
)

var (
	params = &serverParams{
		rawConfig: &config.Config{
			Telemetry:    &config.Telemetry{},
			Network:      &config.Network{},
			TxPool:       &config.TxPool{},
			EventTracker: &config.EventTracker{},
		},
	}
)

var (
	errInvalidNATAddress = errors.New("could not parse NAT IP address")
)

type serverParams struct {
	rawConfig  *config.Config
	configPath string

	libp2pAddress     *net.TCPAddr
	prometheusAddress *net.TCPAddr
	natAddress        net.IP
	dnsAddress        multiaddr.Multiaddr
	grpcAddress       *net.TCPAddr
	jsonRPCAddress    *net.TCPAddr

	blockGasTarget uint64
	devInterval    uint64
	isDevMode      bool

	genesisConfig *chain.Chain
	secretsConfig *secrets.SecretsManagerConfig

	logFileLocation string

	relayer bool
}

func (p *serverParams) isMaxPeersSet() bool {
	return p.rawConfig.Network.MaxPeers != unsetPeersValue
}

func (p *serverParams) isPeerRangeSet() bool {
	return p.rawConfig.Network.MaxInboundPeers != unsetPeersValue ||
		p.rawConfig.Network.MaxOutboundPeers != unsetPeersValue
}

func (p *serverParams) isSecretsConfigPathSet() bool {
	return p.rawConfig.SecretsConfigPath != ""
}

func (p *serverParams) isPrometheusAddressSet() bool {
	return p.rawConfig.Telemetry.PrometheusAddr != ""
}

func (p *serverParams) isNATAddressSet() bool {
	return p.rawConfig.Network.NatAddr != ""
}

func (p *serverParams) isDNSAddressSet() bool {
	return p.rawConfig.Network.DNSAddr != ""
}

func (p *serverParams) isLogFileLocationSet() bool {
	return p.rawConfig.LogFilePath != ""
}

func (p *serverParams) isDevConsensus() bool {
	return server.ConsensusType(p.genesisConfig.Params.GetEngine()) == server.DevConsensus
}

func (p *serverParams) getRestoreFilePath() *string {
	if p.rawConfig.RestoreFile != "" {
		return &p.rawConfig.RestoreFile
	}

	return nil
}

func (p *serverParams) setRawGRPCAddress(grpcAddress string) {
	p.rawConfig.GRPCAddr = grpcAddress
}

func (p *serverParams) setRawJSONRPCAddress(jsonRPCAddress string) {
	p.rawConfig.JSONRPCAddr = jsonRPCAddress
}

func (p *serverParams) setJSONLogFormat(jsonLogFormat bool) {
	p.rawConfig.JSONLogFormat = jsonLogFormat
}

func (p *serverParams) generateConfig() *server.Config {
	return &server.Config{
		Chain: p.genesisConfig,
		JSONRPC: &server.JSONRPC{
			JSONRPCAddr:              p.jsonRPCAddress,
			AccessControlAllowOrigin: p.rawConfig.CorsAllowedOrigins,
			BatchLengthLimit:         p.rawConfig.JSONRPCBatchRequestLimit,
			BlockRangeLimit:          p.rawConfig.JSONRPCBlockRangeLimit,
			ConcurrentRequestsDebug:  p.rawConfig.ConcurrentRequestsDebug,
			WebSocketReadLimit:       p.rawConfig.WebSocketReadLimit,
		},
		GRPCAddr:   p.grpcAddress,
		LibP2PAddr: p.libp2pAddress,
		Telemetry: &server.Telemetry{
			PrometheusAddr: p.prometheusAddress,
		},
		Network: &network.Config{
			NoDiscover:        p.rawConfig.Network.NoDiscover,
			Addr:              p.libp2pAddress,
			NatAddr:           p.natAddress,
			DNS:               p.dnsAddress,
			DataDir:           p.rawConfig.DataDir,
			MaxPeers:          p.rawConfig.Network.MaxPeers,
			MaxInboundPeers:   p.rawConfig.Network.MaxInboundPeers,
			MaxOutboundPeers:  p.rawConfig.Network.MaxOutboundPeers,
			Chain:             p.genesisConfig,
			GossipMessageSize: p.rawConfig.Network.GossipMessageSize,
		},
		DataDir:            p.rawConfig.DataDir,
		Seal:               p.rawConfig.ShouldSeal,
		PriceLimit:         p.rawConfig.TxPool.PriceLimit,
		MaxSlots:           p.rawConfig.TxPool.MaxSlots,
		MaxAccountEnqueued: p.rawConfig.TxPool.MaxAccountEnqueued,
		TxGossipBatchSize:  p.rawConfig.TxPool.TxGossipBatchSize,
		SecretsManager:     p.secretsConfig,
		RestoreFile:        p.getRestoreFilePath(),
		LogLevel:           hclog.LevelFromString(p.rawConfig.LogLevel),
		JSONLogFormat:      p.rawConfig.JSONLogFormat,
		LogFilePath:        p.logFileLocation,
		UseTLS:             p.rawConfig.UseTLS,
		TLSCertFile:        p.rawConfig.TLSCertFile,
		TLSKeyFile:         p.rawConfig.TLSKeyFile,

		Relayer:         p.relayer,
		MetricsInterval: p.rawConfig.MetricsInterval,
		EventTracker: &server.EventTracker{
			SyncBatchSize:          p.rawConfig.EventTracker.SyncBatchSize,
			NumBlockConfirmations:  p.rawConfig.EventTracker.NumBlockConfirmations,
			NumOfBlocksToReconcile: p.rawConfig.EventTracker.NumOfBlocksToReconcile,
		},
	}
}
