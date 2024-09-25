package bridge

import (
	"errors"
	"fmt"
	"math/big"
	"net"
	"path"
	"strings"

	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
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

	eventChBuffer = 100
)

// BridgeEventRelayer is an interface that defines functions for bridge event relayer
type BridgeEventRelayer interface {
	state.EventSubscriber
	AddLog(chainID *big.Int, eventLog *ethgo.Log) error
	Close()
	Start(runtimeCfg *config.Runtime, eventProvider *state.EventProvider) error
}

var _ BridgeEventRelayer = (*dummyBridgeEventRelayer)(nil)

// dummyBridgeEventRelayer is a dummy implementation of a BridgeEventRelayer
type dummyBridgeEventRelayer struct{}

func (d *dummyBridgeEventRelayer) AddLog(chainID *big.Int, eventLog *ethgo.Log) error { return nil }
func (d *dummyBridgeEventRelayer) GetLogFilters() map[types.Address][]types.Hash {
	return make(map[types.Address][]types.Hash)
}
func (d *dummyBridgeEventRelayer) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	return nil
}
func (d *dummyBridgeEventRelayer) Close() {}
func (d *dummyBridgeEventRelayer) Start(runtimeCfg *config.Runtime, eventProvider *state.EventProvider) error {
	return nil
}

var _ BridgeEventRelayer = (*bridgeEventRelayerImpl)(nil)

type bridgeEventRelayerImpl struct {
	key    crypto.Key
	logger hclog.Logger

	state *BridgeManagerStore

	externalTxRelayers map[uint64]txrelayer.TxRelayer
	internalTxRelayer  txrelayer.TxRelayer
	internalChainID    *big.Int

	blockchain polychain.Blockchain

	bridgeConfig  map[uint64]*config.Bridge
	eventTrackers []*tracker.EventTracker

	eventCh chan contractsapi.StructAbi
	quitCh  chan struct{}
}

// newBridgeEventRelayer creates a new instance of bridge event relayer
// if the node is not a relayer, it will return a dummy bridge event relayer
func newBridgeEventRelayer(
	blockchain polychain.Blockchain,
	runtimeConfig *config.Runtime,
	logger hclog.Logger,
) (BridgeEventRelayer, error) {
	if !runtimeConfig.ConsensusConfig.IsRelayer {
		return &dummyBridgeEventRelayer{}, nil
	}

	relayer := &bridgeEventRelayerImpl{
		key:             wallet.NewEcdsaSigner(runtimeConfig.Key),
		logger:          logger.Named("bridge-relayer"),
		internalChainID: big.NewInt(runtimeConfig.ChainParams.ChainID),
		blockchain:      blockchain,
		eventCh:         make(chan contractsapi.StructAbi, eventChBuffer),
		quitCh:          make(chan struct{}),
	}

	return relayer, nil
}

// sendTx is a goroutine that listens to the event channel and sends the appropriate transactions
func (ber *bridgeEventRelayerImpl) sendTx() {
	for {
		select {
		case <-ber.quitCh:
			return
		case event := <-ber.eventCh:
			switch event := event.(type) {
			case *contractsapi.SignedBridgeMessageBatch:
				if err := ber.sendBridgeMessageBatch(event); err != nil {
					ber.logger.Error("error occurred while sending bridge message batch transaction", "error", err)
				}
			case *contractsapi.SignedValidatorSet:
				if err := ber.sendCommitValidatorSet(event); err != nil {
					ber.logger.Error("error occurred while sending commit validator set transaction", "error", err)
				}
			default:
				ber.logger.Error("unknown event type", "event", event)
			}
		}
	}
}

// sendBridgeMessageBatch sends bridge message batch execute transaction to the external and internal chains
func (ber *bridgeEventRelayerImpl) sendBridgeMessageBatch(event *contractsapi.SignedBridgeMessageBatch) error {
	var (
		txRelayer          = ber.internalTxRelayer
		destinationChainID = event.Batch.DestinationChainID.Uint64()
		to                 = ber.bridgeConfig[destinationChainID].InternalGatewayAddr
		exists             bool
	)

	if event.Batch.DestinationChainID.Cmp(ber.internalChainID) != 0 {
		txRelayer, exists = ber.externalTxRelayers[destinationChainID]
		if !exists {
			return fmt.Errorf("tx relayer for chain %d not found", destinationChainID)
		}

		to = ber.bridgeConfig[destinationChainID].ExternalGatewayAddr
	}

	input, err := (&contractsapi.ReceiveBatchGatewayFn{
		Batch:     event.Batch,
		Signature: event.Signature,
		Bitmap:    event.Bitmap,
	}).EncodeAbi()
	if err != nil {
		return err
	}

	txn := types.NewTx(types.NewLegacyTx(
		types.WithFrom(ber.key.Address()),
		types.WithTo(&to),
		types.WithInput(input),
	))

	receipt, err := txRelayer.SendTransaction(txn, ber.key)
	if err != nil {
		return fmt.Errorf("failed to execute batch transaction on chain: %d, and gateway contract: %s. Error: %w",
			destinationChainID, to, err)
	}

	ber.logger.Info("sent batch transaction",
		"destinationChainID", destinationChainID,
		"gatewayAddr", to,
		"status", types.ReceiptStatus(receipt.Status),
		"txHash", receipt.TransactionHash,
		"blockNumber", receipt.BlockNumber,
	)

	return nil
}

// sendCommitValidatorSet sends commit validator set transaction to the external and internal chains
func (ber *bridgeEventRelayerImpl) sendCommitValidatorSet(event *contractsapi.SignedValidatorSet) error {
	input, err := (&contractsapi.CommitValidatorSetBridgeStorageFn{
		NewValidatorSet: event.NewValidatorSet,
		Signature:       event.Signature,
		Bitmap:          event.Bitmap,
	}).EncodeAbi()
	if err != nil {
		return err
	}

	for chainID, txRelayer := range ber.externalTxRelayers {
		// send commit validator set transaction to the external chains
		bridgeCfg := ber.bridgeConfig[chainID]
		to := bridgeCfg.ExternalGatewayAddr
		txn := types.NewTx(types.NewLegacyTx(
			types.WithFrom(ber.key.Address()),
			types.WithTo(&to),
			types.WithInput(input),
		))

		receipt, err := txRelayer.SendTransaction(txn, ber.key)
		if err != nil {
			// for now just log the error and continue
			ber.logger.Error("failed to send commit validator set transaction to external chain",
				"chainID", chainID, "error", err)

			continue
		}

		ber.logger.Debug("sent commit validator set transaction to external chain",
			"chainID", chainID,
			"gatewayAddr", to,
			"status", types.ReceiptStatus(receipt.Status),
			"txHash", receipt.TransactionHash,
			"blockNumber", receipt.BlockNumber,
		)

		// send commit validator set transaction to the internal chain
		// we create a new txn to force the getting of the nonce and estimation of gas,
		// since the txn for external chain is modified by the external chain tx relayer
		to = ber.bridgeConfig[chainID].InternalGatewayAddr
		txn = types.NewTx(types.NewLegacyTx(
			types.WithFrom(ber.key.Address()),
			types.WithTo(&to),
			types.WithInput(input),
		))

		receipt, err = ber.internalTxRelayer.SendTransaction(txn, ber.key)
		if err != nil {
			// for now just log the error and continue
			ber.logger.Error("failed to send commit validator set transaction to internal chain", "error", err)

			continue
		}

		ber.logger.Debug("sent commit validator set transaction to internal chain",
			"gatewayAddr", to,
			"status", types.ReceiptStatus(receipt.Status),
			"txHash", receipt.TransactionHash,
			"blockNumber", receipt.BlockNumber,
		)
	}

	return nil
}

// Start starts the bridge relayer
func (ber *bridgeEventRelayerImpl) Start(runtimeCfg *config.Runtime, eventProvider *state.EventProvider) error {
	txRelayers := make(map[uint64]txrelayer.TxRelayer, len(runtimeCfg.GenesisConfig.Bridge))
	trackers := make([]*tracker.EventTracker, 0, len(runtimeCfg.GenesisConfig.Bridge))

	// create tx relayer for internal chain
	internalChainTxRelayer, err := createBridgeTxRelayer(runtimeCfg.ConsensusConfig.RPCEndpoint, ber.logger)
	if err != nil {
		return fmt.Errorf("failed to create tx relayer for internal chain: %w", err)
	}

	ber.internalTxRelayer = internalChainTxRelayer

	// create tx relayers and event trackers for external chains
	for chainID, config := range runtimeCfg.GenesisConfig.Bridge {
		txRelayer, err := createBridgeTxRelayer(config.JSONRPCEndpoint, ber.logger)
		if err != nil {
			return err
		}

		txRelayers[chainID] = txRelayer

		tracker, err := ber.startTrackerForChain(chainID, config, runtimeCfg)
		if err != nil {
			return err
		}

		trackers = append(trackers, tracker)
	}

	ber.externalTxRelayers = txRelayers
	ber.eventTrackers = trackers

	// subscribe relayer to events from the internal chain
	eventProvider.Subscribe(ber)

	go ber.sendTx()

	return nil
}

// startTrackerForChain starts a new instance of tracker.EventTracker
// for listening to the events from an external chain
func (ber *bridgeEventRelayerImpl) startTrackerForChain(chainID uint64,
	bridgeCfg *config.Bridge, runtimeCfg *config.Runtime) (*tracker.EventTracker, error) {
	store, err := store.NewBoltDBEventTrackerStore(
		path.Join(runtimeCfg.StateDataDir, fmt.Sprintf("/bridge-event-relayer%d.db", chainID)))
	if err != nil {
		return nil, err
	}

	eventTracker, err := tracker.NewEventTracker(
		&tracker.EventTrackerConfig{
			EventSubscriber:        ber,
			Logger:                 ber.logger,
			RPCEndpoint:            bridgeCfg.JSONRPCEndpoint,
			SyncBatchSize:          runtimeCfg.EventTracker.SyncBatchSize,
			NumBlockConfirmations:  runtimeCfg.EventTracker.NumBlockConfirmations,
			NumOfBlocksToReconcile: runtimeCfg.EventTracker.NumOfBlocksToReconcile,
			PollInterval:           runtimeCfg.GenesisConfig.BlockTrackerPollInterval.Duration,
			LogFilter: map[ethgo.Address][]ethgo.Hash{
				ethgo.Address(bridgeCfg.ExternalGatewayAddr): {
					bridgeMessageResultEventSig,
				},
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

// EventSubscriber implementation

// GetLogFilters returns a map of log filters for getting desired events,
// where the key is the address of contract that emits desired events,
// and the value is a slice of signatures of events we want to get.
// This function is the implementation of EventSubscriber interface
func (ber *bridgeEventRelayerImpl) GetLogFilters() map[types.Address][]types.Hash {
	logFilters := map[types.Address][]types.Hash{
		contracts.BridgeStorageContract: {
			types.Hash(newBatchEventSig),
			types.Hash(newValidatorSetEventSig),
		},
	}

	for _, bridgeCfg := range ber.bridgeConfig {
		logFilters[bridgeCfg.InternalGatewayAddr] = []types.Hash{
			types.Hash(bridgeMessageResultEventSig)}
	}

	return logFilters
}

// ProcessLog is the implementation of EventSubscriber interface,
// used to handle a log defined in GetLogFilters, provided by event provider
func (ber *bridgeEventRelayerImpl) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	switch log.Topics[0] {
	case bridgeMessageResultEventSig:
		var bridgeMessageResultEvent contractsapi.BridgeMessageResultEvent

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
	case newValidatorSetEventSig:
		var newValidatorSetEvent contractsapi.NewValidatorSetStoredEvent

		doesMatch, err := newValidatorSetEvent.ParseLog(log)
		if err != nil {
			return err
		}

		if !doesMatch {
			return nil
		}

		provider, err := ber.blockchain.GetStateProviderForBlock(header)
		if err != nil {
			return err
		}

		ss := systemstate.NewSystemState(contracts.EpochManagerContract, contracts.BridgeStorageContract, provider)

		newValidatorSet, err := ss.GetValidatorSetByNumber(newValidatorSetEvent.ID)
		if err != nil {
			return err
		}

		// since commit validator set transaction is always before new batch transactions in the block
		// the new validator set transaction will be first in the event ch buffer to be sent
		ber.eventCh <- newValidatorSet
	case newBatchEventSig:
		var newBatchEvent contractsapi.NewBatchEvent

		doesMatch, err := newBatchEvent.ParseLog(log)
		if err != nil {
			return err
		}

		if !doesMatch {
			return nil
		}

		provider, err := ber.blockchain.GetStateProviderForBlock(header)
		if err != nil {
			return err
		}

		ss := systemstate.NewSystemState(contracts.EpochManagerContract, contracts.BridgeStorageContract, provider)

		bridgeBatch, err := ss.GetBridgeBatchByNumber(newBatchEvent.ID)
		if err != nil {
			return err
		}

		ber.eventCh <- bridgeBatch
	default:
		return errUnknownBridgeEventRelayerEvent
	}

	return nil
}

// AddLog is EventTracker implementation
// used to handle a log with data from external chain
func (ber *bridgeEventRelayerImpl) AddLog(chainID *big.Int, eventLog *ethgo.Log) error {
	switch eventLog.Topics[0] {
	case bridgeMessageResultEventSig:
		var bridgeMessageResultEvent contractsapi.BridgeMessageResultEvent

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
		} else {
			// TO DO rollback logic
		}
	default:
		return errUnknownBridgeEventRelayerEvent
	}

	return nil
}

func (ber *bridgeEventRelayerImpl) Close() {
	for _, eventTracker := range ber.eventTrackers {
		eventTracker.Close()
	}

	close(ber.quitCh)
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
