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
			struct SignedBridgeMessageBatch {
    			bytes32 rootHash;
    			uint256 startId;
    			uint256 endId;
    			uint256 sourceChainId;
    			uint256 destinationChainId;
    			uint256[2] signature;
    			bytes bitmap;
			}

			mapping(uint256 => SignedBridgeMessageBatch) public batches;
			
			function setBridgeMessage(uint256 _num) public payable {
				SignedBridgeMessageBatch storage signedBatch = batches[_num];
				signedBatch.rootHash = 0x1555ad6149fc39abc7852aad5c3df6b9df7964ac90ffbbcf6206b1eda846c881;
				signedBatch.startId = 1;
				signedBatch.endId = 5;
				signedBatch.sourceChainId = 2;
				signedBatch.destinationChainId = 3;
				signedBatch.signature = [uint256(300), uint256(200)];
				signedBatch.bitmap = "smth";
				signedBatch.threshold = 0;
				signedBatch.isRollback = false;
				batches[_num] = signedBatch;
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

	provider := &stateProvider{transition: transition}

	systemState := NewSystemState(types.ZeroAddress, result.Address, provider)

	batchID := big.NewInt(24)

	input, err := method.Encode([1]interface{}{batchID})
	require.NoError(t, err)

	_, err = provider.Call(ethgo.Address(result.Address), input, &contract.CallOpts{})
	require.NoError(t, err)

	sbmb, err := systemState.GetBridgeBatchByNumber(batchID)
	require.NoError(t, err)

	require.EqualValues(t, &contractsapi.SignedBridgeMessageBatch{
		Threshold:          big.NewInt(0),
		IsRollback:         false,
		RootHash:           types.StringToHash("0x1555ad6149fc39abc7852aad5c3df6b9df7964ac90ffbbcf6206b1eda846c881"),
		StartID:            big.NewInt(1),
		EndID:              big.NewInt(5),
		SourceChainID:      big.NewInt(2),
		DestinationChainID: big.NewInt(3),
		Signature:          [2]*big.Int{big.NewInt(300), big.NewInt(200)},
		Bitmap:             []byte("smth"),
	}, sbmb)
}

func TestSystemState_EncodeAndDecodeStructWithDinamicValues(t *testing.T) {
	t.Parallel()

	svs := &contractsapi.SignedValidatorSet{
		NewValidatorSet: []*contractsapi.Validator{
			{
				Address:     types.StringToAddress("0x518489F9ed41Fc35BCD23407C484F31897067ff0"),
				BlsKey:      [4]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)},
				VotingPower: big.NewInt(100),
			},
			{
				Address:     types.StringToAddress("0x85dA99c8a7C2C95964c8EfD687E95E632Fc533D6"),
				BlsKey:      [4]*big.Int{big.NewInt(5), big.NewInt(6), big.NewInt(7), big.NewInt(8)},
				VotingPower: big.NewInt(200),
			},
			{
				Address:     types.StringToAddress("0x0bb7AA0b4FdC2D2862c088424260e99ed6299148"),
				BlsKey:      [4]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)},
				VotingPower: big.NewInt(300),
			},
		},
		Signature: [2]*big.Int{big.NewInt(300), big.NewInt(200)},
		Bitmap:    []byte("smth"),
	}

	rawSvs, err := svs.EncodeAbi()
	require.NoError(t, err)

	tmp := &contractsapi.SignedValidatorSet{}
	require.NoError(t, tmp.DecodeAbi(rawSvs))
	require.Equal(t, svs, tmp)
}

func TestSystemState_GetValidatorSetByNumber(t *testing.T) {
	t.Parallel()

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
				signedSet.newValidatorSet.push(Validator(0x85dA99c8a7C2C95964c8EfD687E95E632Fc533D6, [uint256(5), uint256(6), uint256(7), uint256(8)], uint256(200)));
				signedSet.newValidatorSet.push(Validator(0x0bb7AA0b4FdC2D2862c088424260e99ed6299148, [uint256(1), uint256(2), uint256(3), uint256(4)], uint256(300)));
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

	bytecode, err := hex.DecodeString(solcContract.Bin)
	require.NoError(t, err)

	transition := NewTestTransition(t, nil)

	result := transition.Create2(types.ZeroAddress, bytecode, big.NewInt(0), 1000000000)
	assert.NoError(t, result.Err)

	provider := &stateProvider{transition: transition}

	systemState := NewSystemState(types.ZeroAddress, result.Address, provider)

	validatorSetID := big.NewInt(1)

	setValidatorSetFn, err := abi.NewMethod("function setValidatorSet(uint256 _num) public payable")
	require.NoError(t, err)

	input, err := setValidatorSetFn.Encode([1]interface{}{validatorSetID})
	require.NoError(t, err)

	_, err = provider.Call(ethgo.Address(result.Address), input, nil)
	require.NoError(t, err)

	svs, err := systemState.GetValidatorSetByNumber(validatorSetID)
	require.NoError(t, err)

	require.Equal(t,
		&contractsapi.Validator{
			Address:     types.StringToAddress("0x518489F9ed41Fc35BCD23407C484F31897067ff0"),
			BlsKey:      [4]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)},
			VotingPower: big.NewInt(100),
		}, svs.NewValidatorSet[0])
	require.Equal(t,
		&contractsapi.Validator{
			Address:     types.StringToAddress("0x85dA99c8a7C2C95964c8EfD687E95E632Fc533D6"),
			BlsKey:      [4]*big.Int{big.NewInt(5), big.NewInt(6), big.NewInt(7), big.NewInt(8)},
			VotingPower: big.NewInt(200),
		}, svs.NewValidatorSet[1])
	require.Equal(t,
		&contractsapi.Validator{
			Address:     types.StringToAddress("0x0bb7AA0b4FdC2D2862c088424260e99ed6299148"),
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
