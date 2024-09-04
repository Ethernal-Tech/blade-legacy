package polybft

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/hashicorp/go-hclog"
	bolt "go.etcd.io/bbolt"
)

var (
	errUnknownStateSyncRelayerEvent = errors.New("unknown event from gateway contract")

	bridgeMessageResultEventSignature = new(contractsapi.BridgeMessageResultEvent).Sig()
)

// StateSyncRelayer is an interface that defines functions for state sync relayer
type StateSyncRelayer interface {
	EventSubscriber
	PostBlock(req *PostBlockRequest) error
	Init() error
	Close()
}

var _ StateSyncRelayer = (*dummyStateSyncRelayer)(nil)

// dummyStateSyncRelayer is a dummy implementation of a StateSyncRelayer
type dummyStateSyncRelayer struct{}

func (d *dummyStateSyncRelayer) PostBlock(req *PostBlockRequest) error { return nil }
func (d *dummyStateSyncRelayer) Init() error                           { return nil }
func (d *dummyStateSyncRelayer) Close()                                {}

// EventSubscriber implementation
func (d *dummyStateSyncRelayer) GetLogFilters() map[types.Address][]types.Hash {
	return make(map[types.Address][]types.Hash)
}
func (d *dummyStateSyncRelayer) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	return nil
}

var _ StateSyncRelayer = (*stateSyncRelayerImpl)(nil)

type stateSyncRelayerImpl struct {
	*relayerEventsProcessor

	txRelayer txrelayer.TxRelayer
	key       crypto.Key
	logger    hclog.Logger

	notifyCh chan struct{}
	closeCh  chan struct{}
}

func newStateSyncRelayer(
	txRelayer txrelayer.TxRelayer,
	state *BridgeMessageStore,
	blockchain blockchainBackend,
	key crypto.Key,
	config *relayerConfig,
	logger hclog.Logger,
) *stateSyncRelayerImpl {
	relayer := &stateSyncRelayerImpl{
		txRelayer: txRelayer,
		key:       key,
		closeCh:   make(chan struct{}),
		notifyCh:  make(chan struct{}, 1),
		logger:    logger,
		relayerEventsProcessor: &relayerEventsProcessor{
			state:      state,
			logger:     logger,
			config:     config,
			blockchain: blockchain,
		},
	}

	relayer.relayerEventsProcessor.sendTx = relayer.sendTx

	return relayer
}

func (ssr *stateSyncRelayerImpl) Init() error {
	// start consumer
	go func() {
		for {
			select {
			case <-ssr.closeCh:
				return
			case <-ssr.notifyCh:
				ssr.processEvents()
			}
		}
	}()

	return nil
}

func (ssr *stateSyncRelayerImpl) Close() {
	close(ssr.closeCh)
}

func (ssr *stateSyncRelayerImpl) PostBlock(req *PostBlockRequest) error {
	select {
	case ssr.notifyCh <- struct{}{}:
	default:
	}

	return nil
}

func (ssr stateSyncRelayerImpl) sendTx(events []*RelayerEventMetaData) error {
	input, err := (&contractsapi.ReceiveBatchGatewayFn{
		Batch: &contractsapi.BridgeMessageBatch{},
	}).EncodeAbi()
	if err != nil {
		return err
	}

	txn := types.NewTx(types.NewLegacyTx(
		types.WithFrom(ssr.key.Address()),
		types.WithTo(&ssr.config.eventExecutionAddr),
		types.WithGas(types.StateTransactionGasLimit),
		types.WithInput(input),
	))

	// send batchExecute state sync
	_, err = ssr.txRelayer.SendTransaction(txn, ssr.key)

	return err
}

// EventSubscriber implementation

// GetLogFilters returns a map of log filters for getting desired events,
// where the key is the address of contract that emits desired events,
// and the value is a slice of signatures of events we want to get.
// This function is the implementation of EventSubscriber interface
func (ssr *stateSyncRelayerImpl) GetLogFilters() map[types.Address][]types.Hash {
	return map[types.Address][]types.Hash{
		contracts.GatewayContract: {
			types.Hash(new(contractsapi.BridgeMessageResultEvent).Sig()),
		},
	}
}

// ProcessLog is the implementation of EventSubscriber interface,
// used to handle a log defined in GetLogFilters, provided by event provider
func (ssr *stateSyncRelayerImpl) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	var (
		bridgeMessageResultEvent contractsapi.BridgeMessageResultEvent
	)

	switch log.Topics[0] {
	case bridgeMessageResultEventSignature:
		_, err := bridgeMessageResultEvent.ParseLog(log)
		if err != nil {
			return err
		}

		eventID := bridgeMessageResultEvent.Counter.Uint64()

		if bridgeMessageResultEvent.Status {
			ssr.logger.Debug("bridge message result event has been processed", "block", header.Number, "bridgeMsgID", eventID)

			return ssr.state.UpdateRelayerEvents(nil, []*RelayerEventMetaData{{EventID: eventID}}, dbTx)
		}

		ssr.logger.Debug("bridge message result event failed to process", "block", header.Number,
			"bridgeMsgID", eventID, "reason", string(bridgeMessageResultEvent.Message))

		return nil

	default:
		return errUnknownStateSyncRelayerEvent
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
