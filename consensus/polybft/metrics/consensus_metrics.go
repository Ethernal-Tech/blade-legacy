package metrics

import (
	"time"

	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/armon/go-metrics"
)

const (
	// ConsensusMetricsPrefix is a consensus-related metrics prefix
	ConsensusMetricsPrefix = "consensus"
)

// UpdateBlockMetrics updates various metrics based on the given block
// (such as block interval, number of transactions and block rounds metrics)
func UpdateBlockMetrics(currentBlock *types.Block, parentHeader *types.Header) error {
	if currentBlock.Number() > 1 {
		parentTime := time.Unix(int64(parentHeader.Timestamp), 0)        //nolint:gosec
		headerTime := time.Unix(int64(currentBlock.Header.Timestamp), 0) //nolint:gosec
		// update the block interval metric
		metrics.SetGauge([]string{ConsensusMetricsPrefix, "block_interval"}, float32(headerTime.Sub(parentTime).Seconds()))
	}

	// update the number of transactions in the block metric
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "num_txs"}, float32(len(currentBlock.Transactions)))

	extra, err := polytypes.GetIbftExtra(currentBlock.Header.ExtraData)
	if err != nil {
		return err
	}

	// number of rounds needed to seal a block
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "rounds"}, float32(extra.BlockMetaData.BlockRound))
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "chain_head"}, float32(currentBlock.Number()))
	metrics.IncrCounter([]string{ConsensusMetricsPrefix, "block_counter"}, float32(1))
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "block_space_used"}, float32(currentBlock.Header.GasUsed))

	// Update the base fee metric
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "base_fee"}, float32(currentBlock.Header.BaseFee))

	return nil
}

// UpdateEpochMetrics updates epoch-related metrics
// (e.g. epoch number, validator set length)
func UpdateEpochMetrics(epoch uint64, validatorsLen int) {
	// update epoch number metrics
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "epoch_number"}, float32(epoch))
	// update number of validators metrics
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "validators"}, float32(validatorsLen))
}

// UpdateBlockExecutionMetric updates the block execution metric
func UpdateBlockExecutionMetric(start time.Time) {
	metrics.SetGauge([]string{ConsensusMetricsPrefix, "block_execution_time"},
		float32(time.Now().UTC().Sub(start).Seconds()))
}
