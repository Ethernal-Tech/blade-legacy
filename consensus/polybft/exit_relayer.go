package polybft

import (
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
	state          *CheckpointStore
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
	state *CheckpointStore,
	logger hclog.Logger) *exitRelayer {
	return &exitRelayer{
		key:            key,
		state:          state,
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

func (e *exitRelayer) AddLog(log *ethgo.Log) error {
	e.logger.Info("Received CheckpointSubmittedEvent")

	return nil
}
