package cardanofw

import (
	"encoding/json"
	"fmt"
)

type TestCardanoStats struct {
	Block           uint64 `json:"block"`
	Epoch           uint64 `json:"epoch"`
	Era             string `json:"era"`
	Hash            string `json:"hash"`
	Slot            uint64 `json:"slot"`
	SlotInEpoch     uint64 `json:"slotInEpoch"`
	SlotsToEpochEnd uint64 `json:"slotsToEpochEnd"`
	SyncProgress    string `json:"syncProgress"`
}

func NewTestCardanoStats(bytes []byte) (*TestCardanoStats, error) {
	var testCardanoStats TestCardanoStats

	if err := json.Unmarshal(bytes, &testCardanoStats); err != nil {
		return nil, err
	}

	return &testCardanoStats, nil
}

func (tcs *TestCardanoStats) String() string {
	if tcs == nil {
		return "{ nil }"
	}

	return fmt.Sprintf("{ Block: %d, Hash: %s }", tcs.Block, tcs.Hash)
}
