package polybft

import (
	"errors"
	"fmt"
	"math/big"
	"net"
	"path"
	"strings"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/blockchain-event-tracker/store"
	"github.com/Ethernal-Tech/blockchain-event-tracker/tracker"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

var (
	errUnknownBridgeEventRelayerEvent = errors.New("unknown event from gateway contract")
)

// BridgeEventRelayer is an interface that defines functions for bridge event relayer
type BridgeEventRelayer interface {
	EventSubscriber
	PostBlock(req *PostBlockRequest) error
	AddLog(chainID *big.Int, eventLog *ethgo.Log) error
	Close()
}

var _ BridgeEventRelayer = (*dummyBridgeEventRelayer)(nil)

// dummyBridgeEventRelayer is a dummy implementation of a BridgeEventRelayer
type dummyBridgeEventRelayer struct{}

func (d *dummyBridgeEventRelayer) PostBlock(req *PostBlockRequest) error              { return nil }
func (d *dummyBridgeEventRelayer) AddLog(chainID *big.Int, eventLog *ethgo.Log) error { return nil }
func (d *dummyBridgeEventRelayer) GetLogFilters() map[types.Address][]types.Hash {
	return make(map[types.Address][]types.Hash)
}
func (d *dummyBridgeEventRelayer) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	return nil
}
func (d *dummyBridgeEventRelayer) Close() {}

var _ BridgeEventRelayer = (*bridgeEventRelayerImpl)(nil)

type bridgeEventRelayerImpl struct {
	key    crypto.Key
	logger hclog.Logger

	lastValidatorSet *contractsapi.SignedValidatorSet
	bridgeBatches    []*contractsapi.SignedBridgeMessageBatch

	lastGatewayValidatorSet map[uint64][]*contractsapi.Validator
	state                   *BridgeMessageStore

	txRelayers    map[uint64]txrelayer.TxRelayer
	runtimeConfig *runtimeConfig

	bridgeConfig  map[uint64]*BridgeConfig
	eventTrackers []*tracker.EventTracker
}

// newBridgeEventRelayer creates a new instance of bridge event relayer
// if the node is not a relayer, it will return a dummy bridge event relayer
func newBridgeEventRelayer(
	runtimeConfig *runtimeConfig,
	eventProvider *EventProvider,
	logger hclog.Logger,
) (BridgeEventRelayer, error) {
	if !runtimeConfig.consensusConfig.IsRelayer {
		return &dummyBridgeEventRelayer{}, nil
	}

	relayer := &bridgeEventRelayerImpl{
		key:           wallet.NewEcdsaSigner(runtimeConfig.Key),
		logger:        logger,
		runtimeConfig: runtimeConfig,
	}

	txRelayers := make(map[uint64]txrelayer.TxRelayer, len(runtimeConfig.GenesisConfig.Bridge))
	trackers := make([]*tracker.EventTracker, 0, len(runtimeConfig.GenesisConfig.Bridge))

	// create tx relayer for internal chain
	internalChainTxRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithIPAddress(runtimeConfig.consensusConfig.RPCEndpoint),
		txrelayer.WithWriter(logger.StandardWriter(&hclog.StandardLoggerOptions{})),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create tx relayer for internal chain: %w", err)
	}

	txRelayers[uint64(runtimeConfig.consensusConfig.Params.ChainID)] = internalChainTxRelayer

	// create tx relayers and event trackers for external chains
	for chainID, config := range runtimeConfig.GenesisConfig.Bridge {
		txRelayer, err := createBridgeTxRelayer(config.JSONRPCEndpoint, logger)
		if err != nil {
			return nil, err
		}

		txRelayers[chainID] = txRelayer

		tracker, err := relayer.startTrackerForChain(chainID, config, runtimeConfig)
		if err != nil {
			return nil, err
		}

		trackers = append(trackers, tracker)
	}

	relayer.txRelayers = txRelayers
	relayer.eventTrackers = trackers

	// subscribe relayer to events from the internal chain
	eventProvider.Subscribe(relayer)

	return relayer, nil
}

// startTrackerForChain starts a new instance of tracker.EventTracker
// for listening to the events from an external chain
func (ber *bridgeEventRelayerImpl) startTrackerForChain(chainID uint64,
	bridgeCfg *BridgeConfig, runtimeCfg *runtimeConfig) (*tracker.EventTracker, error) {
	var (
		bridgeMessageResultEventSig    = new(contractsapi.BridgeMessageResultEvent).Sig()
		gatewayNewValidatorSetEventSig = new(contractsapi.NewValidatorSetEvent).Sig()
	)

	store, err := store.NewBoltDBEventTrackerStore(
		path.Join(runtimeCfg.DataDir, fmt.Sprintf("/bridge-event-relayer%d.db", chainID)))
	if err != nil {
		return nil, err
	}

	eventTracker, err := tracker.NewEventTracker(
		&tracker.EventTrackerConfig{
			EventSubscriber:        ber,
			Logger:                 ber.logger,
			RPCEndpoint:            bridgeCfg.JSONRPCEndpoint,
			SyncBatchSize:          runtimeCfg.eventTracker.SyncBatchSize,
			NumBlockConfirmations:  runtimeCfg.eventTracker.NumBlockConfirmations,
			NumOfBlocksToReconcile: runtimeCfg.eventTracker.NumOfBlocksToReconcile,
			PollInterval:           runtimeCfg.GenesisConfig.BlockTrackerPollInterval.Duration,
			LogFilter: map[ethgo.Address][]ethgo.Hash{
				ethgo.Address(bridgeCfg.ExternalGatewayAddr): {
					bridgeMessageResultEventSig,
					gatewayNewValidatorSetEventSig},
			},
		},
		store,
		bridgeCfg.EventTrackerStartBlocks[bridgeCfg.ExternalGatewayAddr],
	)
	if err != nil {
		return nil, err
	}

	return eventTracker, eventTracker.Start()
}

// PostBlock is the implementation of BridgeEventRelayer interface
// Called at the end of each block to send the bridge batches to the external chains
func (ber *bridgeEventRelayerImpl) PostBlock(req *PostBlockRequest) error {
	for _, batch := range ber.bridgeBatches {
		input, err := (&contractsapi.ReceiveBatchGatewayFn{
			Batch:     batch.Batch,
			Signature: batch.Signature,
			Bitmap:    batch.Bitmap,
		}).EncodeAbi()
		if err != nil {
			return err
		}

		destionationChainID := batch.Batch.DestinationChainID.Uint64()

		txn := types.NewTx(types.NewLegacyTx(types.WithFrom(
			ber.key.Address()),
			types.WithTo(&ber.bridgeConfig[destionationChainID].InternalGatewayAddr),
			types.WithGas(types.StateTransactionGasLimit),
			types.WithInput(input)))

		if _, err = ber.txRelayers[destionationChainID].SendTransaction(
			txn,
			ber.key,
		); err != nil {
			return err
		}
	}

	ber.bridgeBatches = make([]*contractsapi.SignedBridgeMessageBatch, 0) // TO DO logic for resend batches

	input, err := (&contractsapi.CommitValidatorSetBridgeStorageFn{
		NewValidatorSet: ber.lastValidatorSet.NewValidatorSet,
		Signature:       ber.lastValidatorSet.Signature,
		Bitmap:          ber.lastValidatorSet.Bitmap,
	}).EncodeAbi()
	if err != nil {
		return err
	}

	for chainID, txRelayer := range ber.txRelayers {
		if !isValidatorSetEqual(ber.lastGatewayValidatorSet[chainID], ber.lastValidatorSet.NewValidatorSet) {
			txn := types.NewTx(types.NewLegacyTx(
				types.WithFrom(ber.key.Address()),
				types.WithTo(&ber.bridgeConfig[chainID].ExternalGatewayAddr),
				types.WithGas(types.StateTransactionGasLimit),
				types.WithInput(input),
			))

			if _, err = txRelayer.SendTransaction(txn, ber.key); err != nil {
				return err
			}
		}
	}

	return nil
}

// EventSubscriber implementation

// GetLogFilters returns a map of log filters for getting desired events,
// where the key is the address of contract that emits desired events,
// and the value is a slice of signatures of events we want to get.
// This function is the implementation of EventSubscriber interface
func (ber *bridgeEventRelayerImpl) GetLogFilters() map[types.Address][]types.Hash {
	logFilters := map[types.Address][]types.Hash{
		contracts.BridgeStorageContract: {
			types.Hash(new(contractsapi.NewBatchEvent).Sig()),
			types.Hash(new(contractsapi.NewValidatorSetEvent).Sig()),
		},
	}

	for _, bridgeCfg := range ber.bridgeConfig {
		logFilters[bridgeCfg.InternalGatewayAddr] = []types.Hash{types.Hash(
			new(contractsapi.BridgeMessageResultEvent).Sig())}
	}

	return logFilters
}

// ProcessLog is the implementation of EventSubscriber interface,
// used to handle a log defined in GetLogFilters, provided by event provider
func (ber *bridgeEventRelayerImpl) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	var (
		bridgeMessageResultEvent contractsapi.BridgeMessageResultEvent
		newBatchEvent            contractsapi.NewBatchEvent
		newValidatorSetEvent     contractsapi.NewValidatorSetStoredEvent
	)

	provider, err := ber.runtimeConfig.blockchain.GetStateProviderForBlock(header)
	if err != nil {
		return err
	}

	systemState := NewSystemState(contracts.EpochManagerContract, contracts.BridgeStorageContract, provider)

	switch log.Topics[0] {
	case bridgeMessageResultEvent.Sig():
		doesMatch, err := bridgeMessageResultEvent.ParseLog(log)
		if err != nil {
			return err
		}

		if !doesMatch {
			return nil
		}

		if !bridgeMessageResultEvent.Status {
			// TO DO rollback logic
		}

		return nil
	case newValidatorSetEvent.Sig():
		newValidatorSet, err := systemState.GetValidatorSetByNumber(newValidatorSetEvent.ID)
		if err != nil {
			return err
		}

		ber.lastValidatorSet = newValidatorSet
	case newBatchEvent.Sig():
		bridgeBatch, err := systemState.GetBridgeBatchByNumber(newBatchEvent.ID)
		if err != nil {
			return err
		}

		ber.bridgeBatches = append(ber.bridgeBatches, bridgeBatch)
	default:
		return errUnknownBridgeEventRelayerEvent
	}

	return nil
}

// AddLog is EventTracker implementation
// used to handle a log with data from external chain
func (ber *bridgeEventRelayerImpl) AddLog(chainID *big.Int, eventLog *ethgo.Log) error {
	bridgeMessageResultEvent := &contractsapi.BridgeMessageResultEvent{}
	gatewayNewValidatorSetEvent := &contractsapi.NewValidatorSetEvent{}

	switch eventLog.Topics[0] {
	case bridgeMessageResultEvent.Sig():
		doesMatch, err := bridgeMessageResultEvent.ParseLog(eventLog)
		if err != nil {
			return err
		}

		if !doesMatch {
			return nil
		}

		if bridgeMessageResultEvent.Status {
			if err := ber.state.removeBridgeEvents(bridgeMessageResultEvent); err != nil {
				return err
			}
		}
	case gatewayNewValidatorSetEvent.Sig():
		doesMatch, err := gatewayNewValidatorSetEvent.ParseLog(eventLog)
		if err != nil {
			return err
		}

		if !doesMatch {
			return nil
		}

		ber.lastGatewayValidatorSet[chainID.Uint64()] = gatewayNewValidatorSetEvent.NewValidatorSet
	}

	return nil
}

func (ber *bridgeEventRelayerImpl) Close() {
	for _, eventTracker := range ber.eventTrackers {
		eventTracker.Close()
	}
}

// createBridgeTxRelayer creates a new instance of txrelayer.TxRelayer
// used for sending transactions to the external chain
func createBridgeTxRelayer(rpcEndpoint string, logger hclog.Logger) (txrelayer.TxRelayer, error) {
	if rpcEndpoint == "" || strings.Contains(rpcEndpoint, "0.0.0.0") {
		_, port, err := net.SplitHostPort(rpcEndpoint)
		if err == nil {
			rpcEndpoint = fmt.Sprintf("http://%s:%s", "127.0.0.1", port)
		} else {
			rpcEndpoint = txrelayer.DefaultRPCAddress
		}
	}

	return txrelayer.NewTxRelayer(
		txrelayer.WithIPAddress(rpcEndpoint), txrelayer.WithNoWaiting(),
		txrelayer.WithWriter(logger.StandardWriter(&hclog.StandardLoggerOptions{})))
}

// convertLog converts types.Log to ethgo.Log
func convertLog(log *types.Log) *ethgo.Log {
	l := &ethgo.Log{
		Address: ethgo.Address(log.Address),
		Data:    make([]byte, len(log.Data)),
		Topics:  make([]ethgo.Hash, len(log.Topics)),
	}

	copy(l.Data, log.Data)

	for i, topic := range log.Topics {
		l.Topics[i] = ethgo.Hash(topic)
	}

	return l
}

func isValidatorSetEqual(gatewayValidatorSet, newValidatorSet []*contractsapi.Validator) bool {
	numberOfValidator := len(gatewayValidatorSet)

	if numberOfValidator != len(newValidatorSet) {
		return false
	}

	for i, validator := range gatewayValidatorSet {
		if validator.Address != newValidatorSet[i].Address {
			return false
		}

		if validator.VotingPower.Cmp(newValidatorSet[i].VotingPower) != 0 {
			return false
		}
	}

	return true
}
