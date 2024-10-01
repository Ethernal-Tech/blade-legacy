package systemstate

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/abi"
	"github.com/Ethernal-Tech/ethgo/contract"
	"github.com/Ethernal-Tech/ethgo/testutil"
	"github.com/Ethernal-Tech/ethgo/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemState_GetNextCommittedIndex(t *testing.T) {
	t.Parallel()

	methods := []string{
		"function setCurrentInternalCommittedIndex(uint256 _chainId, uint256 _index) public payable",
		"function setCurrentExternalCommittedIndex(uint256 _chainId, uint256 _index) public payable",
	}

	var scAbi, err = abi.NewABIFromList(methods)
	require.NoError(t, err)

	cc := &testutil.Contract{}
	cc.AddCallback(func() string {
		return `
		mapping(uint256 => uint256) public lastCommitted;
		mapping(uint256 => uint256) public lastCommittedInternal;
		
		function setCurrentExternalCommittedIndex(uint256 _chainId, uint256 _index) public payable {
			lastCommitted[_chainId] = _index;
		}
			
		function setCurrentInternalCommittedIndex(uint256 _chainId, uint256 _index) public payable {
			lastCommittedInternal[_chainId] = _index;
		}`
	})

	solcContract, err := cc.Compile()
	require.NoError(t, err)

	bin, err := hex.DecodeString(solcContract.Bin)
	require.NoError(t, err)

	transition := NewTestTransition(t, nil)

	// deploy a contract
	result := transition.Create2(types.Address{}, bin, big.NewInt(0), 1000000000)
	assert.NoError(t, result.Err)

	provider := &stateProvider{
		transition: transition,
	}

	systemState := NewSystemState(contracts.EpochManagerContract, result.Address, provider)

	currentExternalCommitIntex := uint64(45)
	input, err := scAbi.GetMethod("setCurrentExternalCommittedIndex").Encode([2]interface{}{0, currentExternalCommitIntex})
	assert.NoError(t, err)

	_, err = provider.Call(ethgo.Address(result.Address), input, &contract.CallOpts{})
	assert.NoError(t, err)

	currentInternalCommitIntex := uint64(102)
	input, err = scAbi.GetMethod("setCurrentInternalCommittedIndex").Encode([2]interface{}{0, currentInternalCommitIntex})
	assert.NoError(t, err)

	_, err = provider.Call(ethgo.Address(result.Address), input, &contract.CallOpts{})
	assert.NoError(t, err)

	nextExternalCommittedIndex, err := systemState.GetNextCommittedIndex(0, External)
	assert.NoError(t, err)
	assert.Equal(t, currentExternalCommitIntex+1, nextExternalCommittedIndex)

	// nextInternalCommittedIndex, err := systemState.GetNextCommittedIndex(0, Internal)
	// assert.NoError(t, err)
	// assert.Equal(t, currentInternalCommitIntex+1, nextInternalCommittedIndex)
}

func TestSystemState_GetEpoch(t *testing.T) {
	t.Skip()
	t.Parallel()

	setEpochMethod, err := abi.NewMethod("function setEpoch(uint256 _epochId) public payable")
	require.NoError(t, err)

	cc := &testutil.Contract{}
	cc.AddCallback(func() string {
		return `
			uint256 public currentEpochId;
			
			function setEpoch(uint256 _epochId) public payable {
				currentEpochId = _epochId;
			}
			`
	})

	solcContract, err := cc.Compile()
	require.NoError(t, err)

	bin, err := hex.DecodeString(solcContract.Bin)
	require.NoError(t, err)

	transition := NewTestTransition(t, nil)

	// deploy a contract
	result := transition.Create2(types.Address{}, bin, big.NewInt(0), 1000000000)
	assert.NoError(t, result.Err)

	provider := &stateProvider{
		transition: transition,
	}

	systemState := NewSystemState(result.Address, contracts.BridgeStorageContract, provider)

	expectedEpoch := uint64(50)
	input, err := setEpochMethod.Encode([1]interface{}{expectedEpoch})
	require.NoError(t, err)

	_, err = provider.Call(ethgo.Address(result.Address), input, &contract.CallOpts{})
	require.NoError(t, err)

	epoch, err := systemState.GetEpoch()
	require.NoError(t, err)
	require.Equal(t, expectedEpoch, epoch)
}

func TestStateProvider_Txn_NotSupported(t *testing.T) {
	t.Parallel()

	provider := &stateProvider{
		transition: NewTestTransition(t, nil),
	}

	key, err := wallet.GenerateKey()
	require.NoError(t, err)

	_, err = provider.Txn(ethgo.ZeroAddress, key, []byte{0x1})
	require.ErrorIs(t, err, errSendTxnUnsupported)
}
