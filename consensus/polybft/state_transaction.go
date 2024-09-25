package polybft

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
)

const abiMethodIDLength = 4

func decodeStateTransaction(txData []byte) (contractsapi.StateTransactionInput, error) {
	if len(txData) < abiMethodIDLength {
		return nil, fmt.Errorf("state transactions have input")
	}

	sig := txData[:abiMethodIDLength]

	var (
		commitBridgeTxFn     contractsapi.CommitBatchBridgeStorageFn
		commitValidatorSetFn contractsapi.CommitValidatorSetBridgeStorageFn
		commitEpochFn        contractsapi.CommitEpochEpochManagerFn
		distributeRewardsFn  contractsapi.DistributeRewardForEpochManagerFn
		obj                  contractsapi.StateTransactionInput
	)

	if bytes.Equal(sig, commitBridgeTxFn.Sig()) {
		// bridge batch
		obj = &BridgeBatchSigned{}
	} else if bytes.Equal(sig, commitEpochFn.Sig()) {
		// commit epoch
		obj = &contractsapi.CommitEpochEpochManagerFn{}
	} else if bytes.Equal(sig, distributeRewardsFn.Sig()) {
		// distribute rewards
		obj = &contractsapi.DistributeRewardForEpochManagerFn{}
	} else if bytes.Equal(sig, commitValidatorSetFn.Sig()) {
		// commit validator set
		obj = &contractsapi.CommitValidatorSetBridgeStorageFn{}
	} else {
		return nil, fmt.Errorf("unknown state transaction")
	}

	if err := obj.DecodeAbi(txData); err != nil {
		return nil, err
	}

	return obj, nil
}
