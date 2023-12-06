package polybft

import (
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/umbracle/ethgo"
)

type ExitRelayer interface {
	AddLog(eventLog *ethgo.Log) error
	PostBlock(req *PostBlockRequest) error
}

var _ ExitRelayer = (*dummyExitRelayer)(nil)

type dummyExitRelayer struct{}

func (d *dummyExitRelayer) AddLog(eventLog *ethgo.Log) error      { return nil }
func (d *dummyExitRelayer) PostBlock(req *PostBlockRequest) error { return nil }

// EventProofRetriever is an interface that exposes function for retrieving exit proof
type EventProofRetriever interface {
	GenerateExitProof(exitID uint64) (types.Proof, error)
}

var _ ExitRelayer = (*exitRelayer)(nil)

type exitRelayer struct {
	key            ethgo.Key
	exitStore      *ExitStore
	blockchain     blockchainBackend
	proofRetriever EventProofRetriever
	txRelayer      txrelayer.TxRelayer
	logger         hclog.Logger

	notifyCh chan struct{}
	closeCh  chan struct{}
}

func newExitRelayer(
	txRelayer txrelayer.TxRelayer,
	key ethgo.Key,
	proofRetriever EventProofRetriever,
	blockchain blockchainBackend,
	exitStore *ExitStore,
	logger hclog.Logger) *exitRelayer {
	return &exitRelayer{
		key:            key,
		exitStore:      exitStore,
		logger:         logger,
		txRelayer:      txRelayer,
		blockchain:     blockchain,
		proofRetriever: proofRetriever,
		closeCh:        make(chan struct{}),
		notifyCh:       make(chan struct{}, 1),
	}
}

func (e *exitRelayer) PostBlock(req *PostBlockRequest) error {
	return nil
}

// AddLog saves the received log from event tracker if it matches a checkpoint submitted event ABI
func (e *exitRelayer) AddLog(eventLog *ethgo.Log) error {
	event := &contractsapi.CheckpointSubmittedEvent{}

	doesMatch, err := event.ParseLog(eventLog)
	if !doesMatch {
		return nil
	}

	e.logger.Info(
		"Add checkpoint submitted event",
		"block", eventLog.BlockNumber,
		"hash", eventLog.TransactionHash,
		"index", eventLog.LogIndex,
	)

	if err != nil {
		e.logger.Error("could not decode checkpoint submitted event", "err", err)

		return err
	}

	return nil
}
