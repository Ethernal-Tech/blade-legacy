package polybft

import (
	"errors"
	"fmt"
	"math/big"
	"net"
	"path"
	"strings"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
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
	errUnknownStateSyncRelayerEvent = errors.New("unknown event from gateway contract")
)

// StateSyncRelayer is an interface that defines functions for state sync relayer
type BridgeEventRelayer interface {
	EventSubscriber
	PostBlock(req *PostBlockRequest) error
	AddLog(chainID *big.Int, eventLog *ethgo.Log) error
	Close()
}

var _ BridgeEventRelayer = (*dummyBridgeEventRelayer)(nil)

// dummyBridgeEventRelayer is a dummy implementation of a StateSyncRelayer
type dummyBridgeEventRelayer struct{}

func (d *dummyBridgeEventRelayer) PostBlock(req *PostBlockRequest) error              { return nil }
func (d *dummyBridgeEventRelayer) AddLog(chainID *big.Int, eventLog *ethgo.Log) error { return nil }

// EventSubscriber implementation
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

	eventTrackerConfigs []*eventTrackerConfig
	bridgeConfig        map[uint64]*BridgeConfig
	eventTrackers       []*tracker.EventTracker
}

func newBridgeEventRelayer(
	txRelayers map[uint64]txrelayer.TxRelayer,
	runtimeConfig *runtimeConfig,
	key crypto.Key,
	logger hclog.Logger,
	eventTrackerConfigs []*eventTrackerConfig,
) *bridgeEventRelayerImpl {
	relayer := &bridgeEventRelayerImpl{
		key:                 key,
		logger:              logger,
		txRelayers:          txRelayers,
		runtimeConfig:       runtimeConfig,
		eventTrackerConfigs: eventTrackerConfigs,
	}

	return relayer
}

func (b *bridgeEventRelayerImpl) initTrackers(runtimeConfig *runtimeConfig) error {
	var bridgeMessageResultEventSig = new(contractsapi.BridgeMessageResultEvent).Sig()
	var gatewayNewValidatorSetEventSig = new(contractsapi.NewValidatorSetEvent).Sig()

	for i, eventTrackerConfig := range b.eventTrackerConfigs {
		store, err := store.NewBoltDBEventTrackerStore(
			path.Join(runtimeConfig.DataDir, fmt.Sprintf("/bridge-event-relayer%d.db", i)))
		if err != nil {
			return err
		}

		eventTracker, err := tracker.NewEventTracker(
			&tracker.EventTrackerConfig{
				EventSubscriber:        b,
				Logger:                 b.logger,
				RPCEndpoint:            eventTrackerConfig.jsonrpcAddr,
				SyncBatchSize:          eventTrackerConfig.EventTracker.SyncBatchSize,
				NumBlockConfirmations:  eventTrackerConfig.NumBlockConfirmations,
				NumOfBlocksToReconcile: eventTrackerConfig.NumOfBlocksToReconcile,
				PollInterval:           eventTrackerConfig.trackerPollInterval,
				LogFilter: map[ethgo.Address][]ethgo.Hash{
					ethgo.Address(eventTrackerConfig.gatewayAddr): {
						bridgeMessageResultEventSig,
						gatewayNewValidatorSetEventSig},
				},
			},
			store,
			eventTrackerConfig.startBlock,
		)
		if err != nil {
			return err
		}

		b.eventTrackers = append(b.eventTrackers, eventTracker)
	}

	return nil
}

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

	ber.bridgeBatches = make([]*contractsapi.SignedBridgeMessageBatch, 0)

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

		if bridgeMessageResultEvent.Status {
			if err := ber.state.removeBridgeEvents(&bridgeMessageResultEvent); err != nil {
				return err
			}

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
		return errUnknownStateSyncRelayerEvent
	}

	return nil
}

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
			ber.state.removeBridgeEvents(bridgeMessageResultEvent)
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

func getBridgeTxRelayer(rpcEndpoint string, logger hclog.Logger) (txrelayer.TxRelayer, error) {
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
