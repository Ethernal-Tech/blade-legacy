package systemstate

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
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

	nextInternalCommittedIndex, err := systemState.GetNextCommittedIndex(0, Internal)
	assert.NoError(t, err)
	assert.Equal(t, currentInternalCommitIntex+1, nextInternalCommittedIndex)
}

func TestSystemState_GetBridgeBatchByNumber(t *testing.T) {
	t.Parallel()

	method, err := abi.NewMethod("function setBridgeMessage(uint256 _num) public payable")
	require.NoError(t, err)

	cc := &testutil.Contract{}
	cc.AddCallback(func() string {
		return `
			event BatchCreated(
				bytes bitmap,
				uint256[2] signatures
			);

			struct BridgeMessage {
				uint256 id;
				uint256 sourceChainId;
				uint256 destinationChainId;
				address sender;
				address receiver;
				bytes payload;
			}

			struct BridgeMessageBatch {
				BridgeMessage[] messages;
				uint256 sourceChainId;
				uint256 destinationChainId;
			}

			struct SignedBridgeMessageBatch {
				BridgeMessageBatch batch;
				uint256[2] signature;
				bytes bitmap;
			}

			mapping(uint256 => SignedBridgeMessageBatch) public batches;
			
			function setBridgeMessage(uint256 _num) public payable {
				SignedBridgeMessageBatch storage signedBatch = batches[_num];
				signedBatch.batch.messages.push(BridgeMessage(15, 2, 3, 0x518489F9ed41Fc35BCD23407C484F31897067ff0, 0x518489F9ed41Fc35BCD23407C484F31897067ff0, "b1"));
				signedBatch.batch.messages.push(BridgeMessage(16, 2, 3, 0x518489F9ed41Fc35BCD23407C484F31897067ff0, 0x518489F9ed41Fc35BCD23407C484F31897067ff0, "b2"));
				signedBatch.batch.messages.push(BridgeMessage(17, 2, 3, 0x518489F9ed41Fc35BCD23407C484F31897067ff0, 0x518489F9ed41Fc35BCD23407C484F31897067ff0, "b3"));
				signedBatch.batch.sourceChainId = 2;
				signedBatch.batch.destinationChainId = 3;
				signedBatch.signature = [uint256(300), uint256(200)];
				signedBatch.bitmap = "smth";
				batches[_num] = signedBatch;

				emit BatchCreated(batches[_num].bitmap, batches[_num].signature);
			}

			function getCommittedBatch(uint256 _num) public view returns (SignedBridgeMessageBatch memory) {
				return batches[_num];
			}
		`
	})

	solcContract, err := cc.Compile()
	require.NoError(t, err)

	bin, err := hex.DecodeString(solcContract.Bin)
	require.NoError(t, err)

	transition := NewTestTransition(t, nil)

	result := transition.Create2(types.Address{}, bin, big.NewInt(0), 1000000000)
	assert.NoError(t, result.Err)

	provider := &stateProvider{
		transition: transition,
	}

	systemState := NewSystemState(types.ZeroAddress, result.Address, provider)

	input, err := method.Encode([1]interface{}{24})
	require.NoError(t, err)

	_, err = provider.Call(ethgo.Address(result.Address), input, &contract.CallOpts{})
	require.NoError(t, err)

	sbmb, err := systemState.GetBridgeBatchByNumber(big.NewInt(24))
	require.NoError(t, err)

	require.Equal(t, big.NewInt(3), sbmb.Batch.DestinationChainID)
	require.Equal(t, big.NewInt(2), sbmb.Batch.SourceChainID)

	require.EqualValues(t, &contractsapi.BridgeMessage{
		ID:                 big.NewInt(15),
		SourceChainID:      big.NewInt(2),
		DestinationChainID: big.NewInt(3),
		Sender:             [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		Receiver:           [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		Payload:            []byte("b1"),
	}, sbmb.Batch.Messages[0])
	require.EqualValues(t, &contractsapi.BridgeMessage{
		ID:                 big.NewInt(16),
		SourceChainID:      big.NewInt(2),
		DestinationChainID: big.NewInt(3),
		Sender:             [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		Receiver:           [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		Payload:            []byte("b2"),
	}, sbmb.Batch.Messages[1])
	require.EqualValues(t, &contractsapi.BridgeMessage{
		ID:                 big.NewInt(17),
		SourceChainID:      big.NewInt(2),
		DestinationChainID: big.NewInt(3),
		Sender:             [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		Receiver:           [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		Payload:            []byte("b3"),
	}, sbmb.Batch.Messages[2])

	require.Equal(t, [2]*big.Int{big.NewInt(300), big.NewInt(200)}, sbmb.Signature)
	require.Equal(t, []byte("smth"), sbmb.Bitmap)
}

func TestSystemState_GetValidatorSetByNumber(t *testing.T) {
	t.Parallel()

	method, err := abi.NewMethod("function setValidatorSet(uint256 _num) public payable")
	require.NoError(t, err)

	cc := &testutil.Contract{}
	cc.AddCallback(func() string {
		return `
			struct Validator {
				address _address;
				uint256[4] blsKey;
				uint256 votingPower;
			}

			struct SignedValidatorSet {
				Validator[] newValidatorSet;
				uint256[2] signature;
				bytes bitmap;
			}

			mapping(uint256 => SignedValidatorSet) public commitedValidatorSets;
			
			function setValidatorSet(uint256 _num) public payable {
				SignedValidatorSet storage signedSet = commitedValidatorSets[_num];
				signedSet.newValidatorSet.push(Validator(0x518489F9ed41Fc35BCD23407C484F31897067ff0, [uint256(1), uint256(2), uint256(3), uint256(4)], uint256(100)));
				signedSet.newValidatorSet.push(Validator(0x518489F9ed41Fc35BCD23407C484F31897067ff0, [uint256(1), uint256(2), uint256(3), uint256(4)], uint256(200)));
				signedSet.newValidatorSet.push(Validator(0x518489F9ed41Fc35BCD23407C484F31897067ff0, [uint256(1), uint256(2), uint256(3), uint256(4)], uint256(300)));
				signedSet.signature = [uint256(300), uint256(200)];
				signedSet.bitmap = "smth";
				commitedValidatorSets[_num] = signedSet;
			}

			function getCommittedValidatorSet(uint256 _num) public view returns (SignedValidatorSet memory) {
				return commitedValidatorSets[_num];
			}
		`
	})

	solcContract, err := cc.Compile()
	require.NoError(t, err)

	bin, err := hex.DecodeString(solcContract.Bin)
	require.NoError(t, err)

	transition := NewTestTransition(t, nil)

	result := transition.Create2(types.Address{}, bin, big.NewInt(0), 1000000000)
	assert.NoError(t, result.Err)

	provider := &stateProvider{
		transition: transition,
	}

	systemState := NewSystemState(types.ZeroAddress, result.Address, provider)

	input, err := method.Encode([1]interface{}{24})
	require.NoError(t, err)

	_, err = provider.Call(ethgo.Address(result.Address), input, &contract.CallOpts{})
	require.NoError(t, err)

	svs, err := systemState.GetValidatorSetByNumber(big.NewInt(24))
	require.NoError(t, err)

	require.EqualValues(t, &contractsapi.Validator{
		Address:     [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		BlsKey:      [4]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)},
		VotingPower: big.NewInt(100),
	}, svs.NewValidatorSet[0])
	require.EqualValues(t, &contractsapi.Validator{
		Address:     [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		BlsKey:      [4]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)},
		VotingPower: big.NewInt(200),
	}, svs.NewValidatorSet[1])
	require.EqualValues(t, &contractsapi.Validator{
		Address:     [20]byte{0x51, 0x84, 0x89, 0xF9, 0xed, 0x41, 0xFc, 0x35, 0xBC, 0xD2, 0x34, 0x07, 0xC4, 0x84, 0xF3, 0x18, 0x97, 0x06, 0x7f, 0xf0},
		BlsKey:      [4]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)},
		VotingPower: big.NewInt(300),
	}, svs.NewValidatorSet[2])

	require.Equal(t, [2]*big.Int{big.NewInt(300), big.NewInt(200)}, svs.Signature)
	require.Equal(t, []byte("smth"), svs.Bitmap)
}

func TestSystemState_GetEpoch(t *testing.T) {
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
