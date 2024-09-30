package config

import (
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
)

// Runtime is a struct that holds configuration data for given consensus runtime
type Runtime struct {
	ChainParams     *chain.Params
	GenesisConfig   *PolyBFT
	Forks           *chain.Forks
	Key             *wallet.Key
	ConsensusConfig *consensus.Config
	EventTracker    *consensus.EventTracker
	StateDataDir    string
}
