package framework

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/genesis"
	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/abi"
)

const (
	// envE2ETestsEnabled signal whether the e2e tests will run
	envE2ETestsEnabled = "E2E_TESTS"

	// envLogsEnabled signal whether the output of the nodes get piped to a log file
	envLogsEnabled = "E2E_LOGS"

	// envLogLevel specifies log level of each node
	envLogLevel = "E2E_LOG_LEVEL"

	// envStdoutEnabled signal whether the output of the nodes get piped to stdout
	envStdoutEnabled = "E2E_STDOUT"

	// prefix for validator directory
	defaultValidatorPrefix = "test-chain-"

	// prefix for non validators directory
	nonValidatorPrefix = "test-non-validator-"

	// NativeTokenMintableTestCfg is the test native token config for Supernets originated native tokens
	NativeTokenMintableTestCfg = "Mintable Edge Coin:MEC:18" //nolint:gosec
)

var (
	addressRegExp = regexp.MustCompile(`\(address\) = 0x([a-fA-F0-9]+)`)
)

type NodeType int

const (
	None      NodeType = 0
	Validator NodeType = 1
	Relayer   NodeType = 2
)

func (nt NodeType) IsSet(value NodeType) bool {
	return nt&value == value
}

func (nt *NodeType) Append(value NodeType) {
	*nt |= value
}

var (
	startTime              int64
	testRewardWalletAddr   = types.StringToAddress("0xFFFFFFFF")
	ProxyContractAdminAddr = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
)

func init() {
	startTime = time.Now().UTC().UnixMilli()
}

func resolveBinary() string {
	bin := os.Getenv("EDGE_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return "blade"
}

type TestClusterConfig struct {
	t *testing.T

	Name                 string
	Premine              []string // address[:amount]
	StakeAmounts         []*big.Int
	BootnodeCount        int
	NonValidatorCount    int
	WithLogs             bool
	WithStdout           bool
	HasBridge            bool
	LogsDir              string
	TmpDir               string
	BlockGasLimit        uint64
	BlockTime            time.Duration
	BurnContract         *polybft.BurnContractInfo
	ValidatorPrefix      string
	Binary               string
	ValidatorSetSize     uint64
	EpochSize            int
	EpochReward          int
	NativeTokenConfigRaw string
	BaseFeeConfig        string
	SecretsCallback      func([]types.Address, *TestClusterConfig)
	BladeAdmin           string
	RewardWallet         string
	PredeployContract    string

	ContractDeployerAllowListAdmin   []types.Address
	ContractDeployerAllowListEnabled []types.Address
	ContractDeployerBlockListAdmin   []types.Address
	ContractDeployerBlockListEnabled []types.Address
	TransactionsAllowListAdmin       []types.Address
	TransactionsAllowListEnabled     []types.Address
	TransactionsBlockListAdmin       []types.Address
	TransactionsBlockListEnabled     []types.Address
	BridgeAllowListAdmin             []types.Address
	BridgeAllowListEnabled           []types.Address
	BridgeBlockListAdmin             []types.Address
	BridgeBlockListEnabled           []types.Address

	NumBlockConfirmations uint64

	InitialTrieDB    string
	InitialStateRoot types.Hash

	IsPropertyTest  bool
	TestRewardToken string

	RootTrackerPollInterval time.Duration

	ProxyContractsAdmin string

	VotingPeriod uint64
	VotingDelay  uint64

	logsDirOnce sync.Once

	TLSCertFile string
	TLSKeyFile  string
}

func (c *TestClusterConfig) Dir(name string) string {
	return filepath.Join(c.TmpDir, name)
}

func (c *TestClusterConfig) GetStdout(name string, custom ...io.Writer) io.Writer {
	writers := []io.Writer{}

	if c.WithLogs {
		c.logsDirOnce.Do(func() {
			c.initLogsDir()
		})

		f, err := os.OpenFile(filepath.Join(c.LogsDir, name+".log"), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			c.t.Fatal(err)
		}

		writers = append(writers, f)

		c.t.Cleanup(func() {
			err = f.Close()
			if err != nil {
				c.t.Logf("Failed to close file. Error: %s", err)
			}
		})
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

func (c *TestClusterConfig) initLogsDir() {
	logsDir := path.Join("../..", fmt.Sprintf("e2e-logs-%d", startTime), c.t.Name())
	if c.IsPropertyTest {
		// property tests run cluster multiple times, so each cluster run will be in the main folder
		// e2e-logs-{someNumber}/NameOfPropertyTest/NameOfPropertyTest-{someNumber}
		// to have a separation between logs of each cluster run
		logsDir = path.Join(logsDir, fmt.Sprintf("%v-%d", c.t.Name(), time.Now().UTC().Unix()))
	}

	if err := common.CreateDirSafe(logsDir, 0750); err != nil {
		c.t.Fatal(err)
	}

	c.t.Logf("logs enabled for e2e test: %s", logsDir)
	c.LogsDir = logsDir
}

func (c *TestClusterConfig) GetProxyContractsAdmin() string {
	proxyAdminAddr := c.ProxyContractsAdmin
	if proxyAdminAddr == "" {
		proxyAdminAddr = ProxyContractAdminAddr
	}

	return proxyAdminAddr
}

func (c *TestClusterConfig) getStakeAmount(validatorIndex int) *big.Int {
	l := len(c.StakeAmounts)
	if l == 0 || l <= validatorIndex || validatorIndex < 0 {
		return command.DefaultStake
	}

	return c.StakeAmounts[validatorIndex]
}

type TestCluster struct {
	Config      *TestClusterConfig
	Servers     []*TestServer
	Bridge      *TestBridge
	initialPort int64

	once         sync.Once
	failCh       chan struct{}
	executionErr error
}

type ClusterOption func(*TestClusterConfig)

func WithPremine(addresses ...types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		for _, a := range addresses {
			h.Premine = append(h.Premine, a.String())
		}
	}
}

func WithSecretsCallback(fn func([]types.Address, *TestClusterConfig)) ClusterOption {
	return func(h *TestClusterConfig) {
		h.SecretsCallback = fn
	}
}

func WithNonValidators(num int) ClusterOption {
	return func(h *TestClusterConfig) {
		h.NonValidatorCount = num
	}
}

func WithValidatorSnapshot(validatorsLen uint64) ClusterOption {
	return func(h *TestClusterConfig) {
		h.ValidatorSetSize = validatorsLen
	}
}

func WithBridge() ClusterOption {
	return func(h *TestClusterConfig) {
		h.HasBridge = true
	}
}

func WithBaseFeeConfig(config string) ClusterOption {
	return func(h *TestClusterConfig) {
		if config == "" {
			h.BaseFeeConfig = command.DefaultGenesisBaseFeeConfig
		} else {
			h.BaseFeeConfig = config
		}
	}
}

func WithGenesisState(databasePath string, stateRoot types.Hash) ClusterOption {
	return func(h *TestClusterConfig) {
		h.InitialTrieDB = databasePath
		h.InitialStateRoot = stateRoot
	}
}

func WithBootnodeCount(cnt int) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BootnodeCount = cnt
	}
}

func WithEpochSize(epochSize int) ClusterOption {
	return func(h *TestClusterConfig) {
		h.EpochSize = epochSize
	}
}

func WithEpochReward(epochReward int) ClusterOption {
	return func(h *TestClusterConfig) {
		h.EpochReward = epochReward
	}
}

func WithBlockTime(blockTime time.Duration) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BlockTime = blockTime
	}
}

func WithBlockGasLimit(blockGasLimit uint64) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BlockGasLimit = blockGasLimit
	}
}

func WithBurnContract(burnContract *polybft.BurnContractInfo) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BurnContract = burnContract
	}
}

func WithNumBlockConfirmations(numBlockConfirmations uint64) ClusterOption {
	return func(h *TestClusterConfig) {
		h.NumBlockConfirmations = numBlockConfirmations
	}
}

func WithContractDeployerAllowListAdmin(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.ContractDeployerAllowListAdmin = append(h.ContractDeployerAllowListAdmin, addr)
	}
}

func WithContractDeployerAllowListEnabled(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.ContractDeployerAllowListEnabled = append(h.ContractDeployerAllowListEnabled, addr)
	}
}

func WithContractDeployerBlockListAdmin(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.ContractDeployerBlockListAdmin = append(h.ContractDeployerBlockListAdmin, addr)
	}
}

func WithContractDeployerBlockListEnabled(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.ContractDeployerBlockListEnabled = append(h.ContractDeployerBlockListEnabled, addr)
	}
}

func WithTransactionsAllowListAdmin(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.TransactionsAllowListAdmin = append(h.TransactionsAllowListAdmin, addr)
	}
}

func WithTransactionsAllowListEnabled(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.TransactionsAllowListEnabled = append(h.TransactionsAllowListEnabled, addr)
	}
}

func WithTransactionsBlockListAdmin(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.TransactionsBlockListAdmin = append(h.TransactionsBlockListAdmin, addr)
	}
}

func WithTransactionsBlockListEnabled(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.TransactionsBlockListEnabled = append(h.TransactionsBlockListEnabled, addr)
	}
}

func WithBridgeAllowListAdmin(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BridgeAllowListAdmin = append(h.BridgeAllowListAdmin, addr)
	}
}

func WithBridgeAllowListEnabled(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BridgeAllowListEnabled = append(h.BridgeAllowListEnabled, addr)
	}
}

func WithBridgeBlockListAdmin(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BridgeBlockListAdmin = append(h.BridgeBlockListAdmin, addr)
	}
}

func WithBridgeBlockListEnabled(addr types.Address) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BridgeBlockListEnabled = append(h.BridgeBlockListEnabled, addr)
	}
}

func WithPropertyTestLogging() ClusterOption {
	return func(h *TestClusterConfig) {
		h.IsPropertyTest = true
	}
}

func WithNativeTokenConfig(tokenConfigRaw string) ClusterOption {
	return func(h *TestClusterConfig) {
		h.NativeTokenConfigRaw = tokenConfigRaw
	}
}

func WithTestRewardToken() ClusterOption {
	return func(h *TestClusterConfig) {
		h.TestRewardToken = hex.EncodeToString(contractsapi.TestRewardToken.DeployedBytecode)
	}
}

func WithRootTrackerPollInterval(pollInterval time.Duration) ClusterOption {
	return func(h *TestClusterConfig) {
		h.RootTrackerPollInterval = pollInterval
	}
}

func WithProxyContractsAdmin(address string) ClusterOption {
	return func(h *TestClusterConfig) {
		h.ProxyContractsAdmin = address
	}
}

func WithBladeAdmin(address string) ClusterOption {
	return func(h *TestClusterConfig) {
		h.BladeAdmin = address
	}
}

func WithGovernanceVotingPeriod(votingPeriod uint64) ClusterOption {
	return func(h *TestClusterConfig) {
		h.VotingPeriod = votingPeriod
	}
}

func WithGovernanceVotingDelay(votingDelay uint64) ClusterOption {
	return func(h *TestClusterConfig) {
		h.VotingDelay = votingDelay
	}
}

func WithRewardWallet(rewardWallet string) ClusterOption {
	return func(h *TestClusterConfig) {
		h.RewardWallet = rewardWallet
	}
}

func WithPredeploy(predeployString string) ClusterOption {
	return func(h *TestClusterConfig) {
		h.PredeployContract = predeployString
	}
}

func WithHTTPS(certFile string, keyFile string) ClusterOption {
	return func(h *TestClusterConfig) {
		h.TLSCertFile = certFile
		h.TLSKeyFile = keyFile
	}
}

func isTrueEnv(e string) bool {
	return strings.ToLower(os.Getenv(e)) == "true"
}

func NewPropertyTestCluster(t *testing.T, validatorsCount int, opts ...ClusterOption) *TestCluster {
	t.Helper()

	opts = append(opts, WithPropertyTestLogging())

	return NewTestCluster(t, validatorsCount, opts...)
}

func NewTestCluster(t *testing.T, validatorsCount int, opts ...ClusterOption) *TestCluster {
	t.Helper()

	var err error

	config := &TestClusterConfig{
		t:             t,
		WithLogs:      isTrueEnv(envLogsEnabled),
		WithStdout:    isTrueEnv(envStdoutEnabled),
		Binary:        resolveBinary(),
		EpochSize:     10,
		EpochReward:   1,
		BlockGasLimit: 1e7, // 10M
		StakeAmounts:  []*big.Int{},
		HasBridge:     false,
		VotingDelay:   10,
	}

	if config.ValidatorPrefix == "" {
		config.ValidatorPrefix = defaultValidatorPrefix
	}

	for _, opt := range opts {
		opt(config)
	}

	if !isTrueEnv(envE2ETestsEnabled) {
		var testType string
		if config.IsPropertyTest {
			testType = "property"
		} else {
			testType = "integration"
		}

		t.Skipf("%s tests are disabled.", testType)
	}

	config.TmpDir, err = os.MkdirTemp("/tmp", "e2e-polybft-")
	require.NoError(t, err)

	cluster := &TestCluster{
		Servers:     []*TestServer{},
		Config:      config,
		initialPort: 30300,
		failCh:      make(chan struct{}),
		once:        sync.Once{},
	}

	// in case no validators are specified in opts, all nodes will be validators
	if cluster.Config.ValidatorSetSize == 0 {
		cluster.Config.ValidatorSetSize = uint64(validatorsCount)
	}

	// run init accounts for validators
	addresses, err := cluster.InitSecrets(cluster.Config.ValidatorPrefix, int(cluster.Config.ValidatorSetSize))
	require.NoError(t, err)

	if cluster.Config.SecretsCallback != nil {
		cluster.Config.SecretsCallback(addresses, cluster.Config)
	}

	if config.NonValidatorCount > 0 {
		// run init accounts for non-validators
		// we don't call secrets callback on non-validators,
		// since we have nothing to premine nor stake for non validators
		_, err = cluster.InitSecrets(nonValidatorPrefix, config.NonValidatorCount)
		require.NoError(t, err)
	}

	genesisPath := path.Join(config.TmpDir, "genesis.json")

	{
		// run genesis configuration population
		args := []string{
			"genesis",
			"--validators-path", config.TmpDir,
			"--validators-prefix", cluster.Config.ValidatorPrefix,
			"--dir", genesisPath,
			"--block-gas-limit", strconv.FormatUint(cluster.Config.BlockGasLimit, 10),
			"--epoch-size", strconv.Itoa(cluster.Config.EpochSize),
			"--epoch-reward", strconv.Itoa(cluster.Config.EpochReward),
			"--premine", "0x0000000000000000000000000000000000000000",
			"--trieroot", cluster.Config.InitialStateRoot.String(),
			"--vote-delay", fmt.Sprint(cluster.Config.VotingDelay),
		}

		bladeAdmin := cluster.Config.BladeAdmin
		if cluster.Config.BladeAdmin == "" {
			bladeAdmin = addresses[0].String()
		}

		args = append(args, "--blade-admin", bladeAdmin)

		if cluster.Config.RewardWallet != "" {
			args = append(args, "--reward-wallet", cluster.Config.RewardWallet)
		} else {
			args = append(args, "--reward-wallet", testRewardWalletAddr.String())
		}

		if cluster.Config.VotingPeriod > 0 {
			args = append(args, "--vote-period", fmt.Sprint(cluster.Config.VotingPeriod))
		}

		if cluster.Config.BlockTime != 0 {
			args = append(args, "--block-time",
				cluster.Config.BlockTime.String())
		}

		if cluster.Config.RootTrackerPollInterval != 0 {
			args = append(args, "--block-tracker-poll-interval",
				cluster.Config.RootTrackerPollInterval.String())
		}

		if cluster.Config.TestRewardToken != "" {
			args = append(args, "--reward-token-code", cluster.Config.TestRewardToken)
		}

		if cluster.Config.BaseFeeConfig != "" {
			args = append(args, "--base-fee-config", cluster.Config.BaseFeeConfig)
		}

		if cluster.Config.NativeTokenConfigRaw != "" {
			args = append(args, "--native-token-config", cluster.Config.NativeTokenConfigRaw)
		}

		tokenConfig, err := polybft.ParseRawTokenConfig(cluster.Config.NativeTokenConfigRaw)
		require.NoError(t, err)

		if len(cluster.Config.Premine) != 0 && tokenConfig.IsMintable {
			// only add premine flags in genesis if token is mintable
			for _, premine := range cluster.Config.Premine {
				args = append(args, "--premine", premine)
			}
		}

		burnContract := cluster.Config.BurnContract
		if burnContract != nil {
			args = append(args, "--burn-contract",
				fmt.Sprintf("%d:%s:%s",
					burnContract.BlockNumber, burnContract.Address, burnContract.DestinationAddress))
		}

		if tokenConfig.IsMintable && len(cluster.Config.StakeAmounts) != 0 {
			for i, addr := range addresses {
				args = append(args, "--stake", fmt.Sprintf("%s:%s", addr.String(), cluster.Config.getStakeAmount(i).String()))
			}
		}

		validators, err := genesis.ReadValidatorsByPrefix(
			cluster.Config.TmpDir, cluster.Config.ValidatorPrefix, nil, true)
		require.NoError(t, err)

		if cluster.Config.BootnodeCount > 0 {
			bootNodesCnt := cluster.Config.BootnodeCount
			if len(validators) < bootNodesCnt {
				bootNodesCnt = len(validators)
			}

			for i := 0; i < bootNodesCnt; i++ {
				args = append(args, "--bootnode", validators[i].MultiAddr)
			}
		}

		if len(cluster.Config.ContractDeployerAllowListAdmin) != 0 {
			args = append(args, "--contract-deployer-allow-list-admin",
				strings.Join(sliceAddressToSliceString(cluster.Config.ContractDeployerAllowListAdmin), ","))
		}

		if len(cluster.Config.ContractDeployerAllowListEnabled) != 0 {
			args = append(args, "--contract-deployer-allow-list-enabled",
				strings.Join(sliceAddressToSliceString(cluster.Config.ContractDeployerAllowListEnabled), ","))
		}

		if len(cluster.Config.ContractDeployerBlockListAdmin) != 0 {
			args = append(args, "--contract-deployer-block-list-admin",
				strings.Join(sliceAddressToSliceString(cluster.Config.ContractDeployerBlockListAdmin), ","))
		}

		if len(cluster.Config.ContractDeployerBlockListEnabled) != 0 {
			args = append(args, "--contract-deployer-block-list-enabled",
				strings.Join(sliceAddressToSliceString(cluster.Config.ContractDeployerBlockListEnabled), ","))
		}

		if len(cluster.Config.TransactionsAllowListAdmin) != 0 {
			args = append(args, "--transactions-allow-list-admin",
				strings.Join(sliceAddressToSliceString(cluster.Config.TransactionsAllowListAdmin), ","))
		}

		if len(cluster.Config.TransactionsAllowListEnabled) != 0 {
			args = append(args, "--transactions-allow-list-enabled",
				strings.Join(sliceAddressToSliceString(cluster.Config.TransactionsAllowListEnabled), ","))
		}

		if len(cluster.Config.TransactionsBlockListAdmin) != 0 {
			args = append(args, "--transactions-block-list-admin",
				strings.Join(sliceAddressToSliceString(cluster.Config.TransactionsBlockListAdmin), ","))
		}

		if len(cluster.Config.TransactionsBlockListEnabled) != 0 {
			args = append(args, "--transactions-block-list-enabled",
				strings.Join(sliceAddressToSliceString(cluster.Config.TransactionsBlockListEnabled), ","))
		}

		if cluster.Config.HasBridge {
			if len(cluster.Config.BridgeAllowListAdmin) != 0 {
				args = append(args, "--bridge-allow-list-admin",
					strings.Join(sliceAddressToSliceString(cluster.Config.BridgeAllowListAdmin), ","))
			}

			if len(cluster.Config.BridgeAllowListEnabled) != 0 {
				args = append(args, "--bridge-allow-list-enabled",
					strings.Join(sliceAddressToSliceString(cluster.Config.BridgeAllowListEnabled), ","))
			}

			if len(cluster.Config.BridgeBlockListAdmin) != 0 {
				args = append(args, "--bridge-block-list-admin",
					strings.Join(sliceAddressToSliceString(cluster.Config.BridgeBlockListAdmin), ","))
			}

			if len(cluster.Config.BridgeBlockListEnabled) != 0 {
				args = append(args, "--bridge-block-list-enabled",
					strings.Join(sliceAddressToSliceString(cluster.Config.BridgeBlockListEnabled), ","))
			}
		}

		proxyAdminAddr := cluster.Config.ProxyContractsAdmin
		if proxyAdminAddr == "" {
			proxyAdminAddr = ProxyContractAdminAddr
		}

		args = append(args, "--proxy-contracts-admin", proxyAdminAddr)

		if config.PredeployContract != "" {
			parts := strings.Split(config.PredeployContract, ":")
			require.Equal(t, 2, len(parts))
			args = append(args, "--stake-token", parts[0])
		}

		// run genesis command with all the arguments
		err = cluster.cmdRun(args...)
		require.NoError(t, err)
	}

	if config.PredeployContract != "" {
		parts := strings.Split(config.PredeployContract, ":")
		require.Equal(t, 2, len(parts))
		// run predeploy genesis population
		args := []string{
			"genesis", "predeploy",
			"--predeploy-address", parts[0],
			"--artifacts-name", parts[1],
			"--chain", genesisPath,
			"--deployer-address", config.BladeAdmin}

		err = cluster.cmdRun(args...)
		require.NoError(t, err)
	}

	if cluster.Config.HasBridge {
		// start bridge
		cluster.Bridge, err = NewTestBridge(t, cluster.Config)
		require.NoError(t, err)

		// deploy rootchain contracts
		err = cluster.Bridge.deployRootchainContracts(genesisPath)
		require.NoError(t, err)

		polybftConfig, err := polybft.LoadPolyBFTConfig(genesisPath)
		require.NoError(t, err)

		tokenConfig, err := polybft.ParseRawTokenConfig(cluster.Config.NativeTokenConfigRaw)
		require.NoError(t, err)

		// fund addresses on the rootchain
		err = cluster.Bridge.fundAddressesOnRoot(polybftConfig)
		require.NoError(t, err)

		// add premine if token is non-mintable
		err = cluster.Bridge.mintNativeRootToken(addresses, tokenConfig, polybftConfig)
		require.NoError(t, err)

		err = cluster.Bridge.premineNativeRootToken(tokenConfig, polybftConfig)
		require.NoError(t, err)

		// finalize genesis validators on the rootchain
		err = cluster.Bridge.finalizeGenesis(genesisPath, tokenConfig, polybftConfig)
		require.NoError(t, err)
	}

	for i := 1; i <= int(cluster.Config.ValidatorSetSize); i++ {
		nodeType := Validator
		if i == 1 {
			nodeType.Append(Relayer)
		}

		dir := cluster.Config.ValidatorPrefix + strconv.Itoa(i)
		cluster.InitTestServer(t, dir, cluster.Bridge.JSONRPCAddr(), nodeType)
	}

	for i := 1; i <= cluster.Config.NonValidatorCount; i++ {
		dir := nonValidatorPrefix + strconv.Itoa(i)
		cluster.InitTestServer(t, dir, cluster.Bridge.JSONRPCAddr(), None)
	}

	return cluster
}

func (c *TestCluster) InitTestServer(t *testing.T,
	dataDir string, bridgeJSONRPC string, nodeType NodeType) {
	t.Helper()

	logLevel := os.Getenv(envLogLevel)

	dataDir = c.Config.Dir(dataDir)
	if c.Config.InitialTrieDB != "" {
		err := CopyDir(c.Config.InitialTrieDB, filepath.Join(dataDir, "trie"))
		if err != nil {
			t.Fatal(err)
		}
	}

	srv := NewTestServer(t, c.Config, bridgeJSONRPC, func(config *TestServerConfig) {
		config.DataDir = dataDir
		config.Validator = nodeType.IsSet(Validator)
		config.Chain = c.Config.Dir("genesis.json")
		config.P2PPort = c.getOpenPort()
		config.LogLevel = logLevel
		config.Relayer = nodeType.IsSet(Relayer)
		config.NumBlockConfirmations = c.Config.NumBlockConfirmations
		config.BridgeJSONRPC = bridgeJSONRPC
		config.TLSCertFile = c.Config.TLSCertFile
		config.TLSKeyFile = c.Config.TLSKeyFile
	})

	// watch the server for stop signals. It is important to fix the specific
	// 'node' reference since 'TestServer' creates a new one if restarted.
	go func(node *node) {
		<-node.Wait()

		if !node.ExitResult().Signaled {
			c.Fail(fmt.Errorf("server at dir '%s' has stopped unexpectedly", dataDir))
		}
	}(srv.node)

	c.Servers = append(c.Servers, srv)
}

func (c *TestCluster) cmdRun(args ...string) error {
	return runCommand(c.Config.Binary, args, c.Config.GetStdout(args[0]))
}

func (c *TestCluster) Fail(err error) {
	c.once.Do(func() {
		c.executionErr = err
		close(c.failCh)
	})
}

func (c *TestCluster) Stop() {
	if c.Bridge != nil {
		c.Bridge.Stop()
	}

	for _, srv := range c.Servers {
		if srv.isRunning() {
			srv.Stop()
		}
	}
}

func (c *TestCluster) Stats(t *testing.T) {
	t.Helper()

	for index, i := range c.Servers {
		if !i.isRunning() {
			continue
		}

		num, err := i.JSONRPC().BlockNumber()
		t.Log("Stats node", index, "err", err, "block", num, "validator", i.config.Validator)
	}
}

func (c *TestCluster) WaitUntil(timeout, pollFrequency time.Duration, handler func() bool) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return fmt.Errorf("timeout")
		case <-c.failCh:
			return c.executionErr
		case <-time.After(pollFrequency):
		}

		if handler() {
			return nil
		}
	}
}

func (c *TestCluster) WaitForReady(t *testing.T) {
	t.Helper()

	require.NoError(t, c.WaitForBlock(1, time.Minute))
}

func (c *TestCluster) WaitForBlock(n uint64, timeout time.Duration) error {
	timer := time.NewTimer(timeout)

	ok := false
	for !ok {
		select {
		case <-timer.C:
			return fmt.Errorf("wait for block timeout")
		case <-time.After(2 * time.Second):
		}

		ok = true

		for _, i := range c.Servers {
			if !i.isRunning() {
				continue
			}

			num, err := i.JSONRPC().BlockNumber()

			if err != nil || num < n {
				ok = false

				break
			}
		}
	}

	return nil
}

// WaitForGeneric waits until all running servers returns true from fn callback or timeout defined by dur occurs
func (c *TestCluster) WaitForGeneric(dur time.Duration, fn func(*TestServer) bool) error {
	return c.WaitUntil(dur, 2*time.Second, func() bool {
		for _, srv := range c.Servers {
			// query only running servers
			if srv.isRunning() && !fn(srv) {
				return false
			}
		}

		return true
	})
}

func (c *TestCluster) getOpenPort() int64 {
	c.initialPort++

	return c.initialPort
}

// runCommand executes command with given arguments
func runCommand(binary string, args []string, stdout io.Writer) error {
	var stdErr bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Stderr = &stdErr
	cmd.Stdout = stdout

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

// RunEdgeCommand - calls a command line edge function
func RunEdgeCommand(args []string, stdout io.Writer) error {
	return runCommand(resolveBinary(), args, stdout)
}

// InitSecrets initializes account(s) secrets with given prefix.
// (secrets are being stored in the temp directory created by given e2e test execution)
func (c *TestCluster) InitSecrets(prefix string, count int) ([]types.Address, error) {
	var b bytes.Buffer

	args := []string{
		"secrets", "init",
		"--data-dir", path.Join(c.Config.TmpDir, prefix),
		"--num", strconv.Itoa(count),
		"--insecure",
	}
	stdOut := c.Config.GetStdout("secrets-init", &b)

	if err := runCommand(c.Config.Binary, args, stdOut); err != nil {
		return nil, err
	}

	parsed := addressRegExp.FindAllStringSubmatch(b.String(), -1)
	result := make([]types.Address, len(parsed))

	for i, v := range parsed {
		result[i] = types.StringToAddress(v[1])
	}

	return result, nil
}

func (c *TestCluster) ExistsCode(t *testing.T, addr types.Address) bool {
	t.Helper()

	client, err := jsonrpc.NewEthClient(c.Servers[0].JSONRPCAddr())
	require.NoError(t, err)

	code, err := client.GetCode(addr, jsonrpc.LatestBlockNumberOrHash)
	if err != nil {
		return false
	}

	return code != "0x"
}

func (c *TestCluster) Call(t *testing.T, to types.Address, method *abi.Method,
	args ...interface{}) map[string]interface{} {
	t.Helper()

	client, err := jsonrpc.NewEthClient(c.Servers[0].JSONRPCAddr())
	require.NoError(t, err)

	input, err := method.Encode(args)
	require.NoError(t, err)

	msg := &jsonrpc.CallMsg{
		To:   &to,
		Data: input,
	}
	resp, err := client.Call(msg, jsonrpc.LatestBlockNumber, nil)
	require.NoError(t, err)

	data, err := hex.DecodeString(resp[2:])
	require.NoError(t, err)

	output, err := method.Decode(data)
	require.NoError(t, err)

	return output
}

func (c *TestCluster) Deploy(t *testing.T, sender *crypto.ECDSAKey, bytecode []byte) *TestTxn {
	t.Helper()

	tx := types.NewTx(&types.LegacyTx{
		BaseTx: &types.BaseTx{
			From:  sender.Address(),
			Input: bytecode,
		},
	})

	return c.SendTxn(t, sender, tx)
}

func (c *TestCluster) Transfer(t *testing.T, sender *crypto.ECDSAKey, target types.Address, value *big.Int) *TestTxn {
	t.Helper()

	tx := types.NewTx(types.NewLegacyTx(
		types.WithFrom(sender.Address()),
		types.WithValue(value),
		types.WithTo(&target),
	))

	return c.SendTxn(t, sender, tx)
}

func (c *TestCluster) MethodTxn(t *testing.T, sender *crypto.ECDSAKey, target types.Address, input []byte) *TestTxn {
	t.Helper()

	tx := types.NewTx(types.NewLegacyTx(
		types.WithFrom(sender.Address()),
		types.WithInput(input),
		types.WithTo(&target),
	))

	return c.SendTxn(t, sender, tx)
}

// SendTxn sends a transaction
func (c *TestCluster) SendTxn(t *testing.T, sender *crypto.ECDSAKey, txn *types.Transaction) *TestTxn {
	t.Helper()

	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithIPAddress(c.Servers[0].JSONRPCAddr()),
		txrelayer.WithReceiptsTimeout(1*time.Minute),
		txrelayer.WithEstimateGasFallback(),
	)
	require.NoError(t, err)

	receipt, err := txRelayer.SendTransaction(txn, sender)
	if err != nil {
		t.Errorf("failed to send transaction: %s", err.Error())
	}

	return &TestTxn{
		txn:     txn,
		receipt: receipt,
	}
}

type TestTxn struct {
	txn     *types.Transaction
	receipt *ethgo.Receipt
}

// Txn returns the raw transaction that was sent
func (t *TestTxn) Txn() *types.Transaction {
	return t.txn
}

// Receipt returns the receipt of the transaction
func (t *TestTxn) Receipt() *ethgo.Receipt {
	return t.receipt
}

// Succeed returns whether the transaction succeed and it was not reverted
func (t *TestTxn) Succeed() bool {
	return t.receipt != nil && t.receipt.Status == uint64(types.ReceiptSuccess)
}

// Failed returns whether the transaction failed
func (t *TestTxn) Failed() bool {
	return t.receipt == nil || t.receipt.Status == uint64(types.ReceiptFailed)
}

// Reverted returns whether the transaction failed and was reverted consuming
// all the gas from the call
func (t *TestTxn) Reverted() bool {
	return t.Failed() && t.txn.Gas() == t.receipt.GasUsed
}

func sliceAddressToSliceString(addrs []types.Address) []string {
	res := make([]string, len(addrs))
	for indx, addr := range addrs {
		res[indx] = addr.String()
	}

	return res
}

func CopyDir(source, destination string) error {
	err := os.Mkdir(destination, 0755)
	if err != nil {
		return err
	}

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		relPath := strings.Replace(path, source, "", 1)
		if relPath == "" {
			return nil
		}

		data, err := os.ReadFile(filepath.Join(source, relPath))
		if err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(destination, relPath), data, 0600)
	})
}
