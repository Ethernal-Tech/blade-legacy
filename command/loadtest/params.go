package loadtest

import (
	"errors"
	"time"

	"github.com/0xPolygon/polygon-edge/loadtest/runner"
)

const (
	mnemonicFlag     = "mnemonic"
	loadTestTypeFlag = "type"
	loadTestNameFlag = "name"

	receiptsTimeoutFlag = "receipts-timeout"
	txPoolTimeoutFlag   = "txpool-timeout"

	vusFlag        = "vus"
	txsPerUserFlag = "txs-per-user"
	dynamicTxsFlag = "dynamic"
	batchSizeFlag  = "batch-size"

	saveToJSONFlag           = "to-json"
	waitForTxPoolToEmptyFlag = "wait-txpool"
)

var (
	errNoMnemonicProvided      = errors.New("no mnemonic provided")
	errNoLoadTestTypeProvided  = errors.New("no load test type provided")
	errUnsupportedLoadTestType = errors.New("unsupported load test type")
	errInvalidVUs              = errors.New("vus must be greater than 0")
	errInvalidTxsPerUser       = errors.New("txs-per-user must be greater than 0")
	errInvalidBatchSize        = errors.New("batch-size must be greater than 0 and less or equal to txs-per-user")
)

type loadTestParams struct {
	mnemonic       string
	loadTestType   string
	loadTestName   string
	jsonRPCAddress string

	receiptsTimeout time.Duration
	txPoolTimeout   time.Duration

	vus        int
	txsPerUser int
	batchSize  int

	dynamicTxs           bool
	toJSON               bool
	waitForTxPoolToEmpty bool
}

func (ltp *loadTestParams) validateFlags() error {
	if ltp.mnemonic == "" {
		return errNoMnemonicProvided
	}

	if ltp.loadTestType == "" {
		return errNoLoadTestTypeProvided
	}

	if !runner.IsLoadTestSupported(ltp.loadTestType) {
		return errUnsupportedLoadTestType
	}

	if ltp.vus < 1 {
		return errInvalidVUs
	}

	if ltp.txsPerUser < 1 {
		return errInvalidTxsPerUser
	}

	if ltp.batchSize < 1 || ltp.batchSize > ltp.txsPerUser {
		return errInvalidBatchSize
	}

	return nil
}
