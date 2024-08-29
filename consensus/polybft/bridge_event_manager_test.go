package polybft

import (
	"math/big"
	"math/rand"
	"os"
	"testing"

	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/abi"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
)

func newTestBridgeEventManager(t *testing.T, key *validator.TestValidator, runtime Runtime) *bridgeEventManager {
	t.Helper()

	tmpDir, err := os.MkdirTemp("/tmp", "test-data-dir-state-sync")
	require.NoError(t, err)

	state := newTestState(t)
	require.NoError(t, state.EpochStore.insertEpoch(0, nil, 0))

	topic := &mockTopic{}

	s := newBridgeEventManager(hclog.NewNullLogger(), state,
		&stateSyncConfig{
			dataDir:           tmpDir,
			topic:             topic,
			key:               key.Key(),
			maxCommitmentSize: maxCommitmentSize,
		}, runtime)

	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	return s
}

func TestBridgeEventManager_PostEpoch_BuildBridgeBatch(t *testing.T) {
	t.Parallel()

	vals := validator.NewTestValidators(t, 5)

	t.Run("When node is validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})

		// there are no state syncs
		require.NoError(t, s.buildBridgeBatch(nil, 0))
		require.Nil(t, s.pendingBridgeBatches)

		bridgeMessages10 := generateBridgeMessageEvents(t, 10, 0)

		// add 5 state syncs starting in index 0, it will generate one smaller commitment
		for i := 0; i < 5; i++ {
			require.NoError(t, s.state.BridgeMessageStore.insertBridgeMessageEvent(bridgeMessages10[i]))
		}

		require.NoError(t, s.buildBridgeBatch(nil, 0))
		length := len(s.pendingBridgeBatches[0].BridgeMessageBatch.Messages)
		require.Len(t, s.pendingBridgeBatches, 1)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(4), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[length-1].ID.Uint64())
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].Epoch)

		// add the next 5 state syncs, at that point, so that it generates a larger commitment
		for i := 5; i < 10; i++ {
			require.NoError(t, s.state.BridgeMessageStore.insertBridgeMessageEvent(bridgeMessages10[i]))
		}

		require.NoError(t, s.buildBridgeBatch(nil, 0))
		length = len(s.pendingBridgeBatches[1].BridgeMessageBatch.Messages)
		require.Len(t, s.pendingBridgeBatches, 2)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[1].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(9), s.pendingBridgeBatches[1].BridgeMessageBatch.Messages[length-1].ID.Uint64())
		require.Equal(t, uint64(0), s.pendingBridgeBatches[1].Epoch)

		// the message was sent
		require.NotNil(t, s.config.topic.(*mockTopic).consume())
	})

	t.Run("When node is not validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: false})

		stateSyncs10 := generateBridgeMessageEvents(t, 10, 0)

		// add 5 state syncs starting in index 0, they will be saved to db
		for i := 0; i < 5; i++ {
			require.NoError(t, s.state.BridgeMessageStore.insertBridgeMessageEvent(stateSyncs10[i]))
		}

		// I am not a validator so no commitments should be built
		require.NoError(t, s.buildBridgeBatch(nil, 0))
		require.Len(t, s.pendingBridgeBatches, 0)
	})
}

func TestBridgeEventManager_MessagePool(t *testing.T) {
	t.Parallel()

	vals := validator.NewTestValidators(t, 5)

	t.Run("Old epoch", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})

		s.epoch = 1
		msg := &TransportMessage{
			EpochNumber: 0,
		}

		err := s.saveVote(msg)
		require.NoError(t, err)
	})

	t.Run("Sender is not a validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		badVal := validator.NewTestValidator(t, "a", 0)
		msg, err := newMockMsg().sign(badVal, signer.DomainStateReceiver)
		require.NoError(t, err)

		require.Error(t, s.saveVote(msg))
	})

	t.Run("Invalid epoch", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		val := newMockMsg()
		msg, err := val.sign(vals.GetValidator("0"), signer.DomainStateReceiver)
		require.NoError(t, err)

		// invalid epoch +2
		msg.EpochNumber = 2

		require.NoError(t, s.saveVote(msg))

		// no votes for the current epoch
		votes, err := s.state.BridgeMessageStore.getMessageVotes(0, msg.BatchByte, 0)
		require.NoError(t, err)
		require.Len(t, votes, 0)

		// returns an error for the invalid epoch
		_, err = s.state.BridgeMessageStore.getMessageVotes(1, msg.BatchByte, 0)
		require.Error(t, err)
	})

	t.Run("Sender and signature mismatch", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		// validator signs the msg in behalf of another validator
		val := newMockMsg()
		msg, err := val.sign(vals.GetValidator("0"), signer.DomainStateReceiver)
		require.NoError(t, err)

		msg.From = vals.GetValidator("1").Address().String()
		require.Error(t, s.saveVote(msg))

		// non validator signs the msg in behalf of a validator
		badVal := validator.NewTestValidator(t, "a", 0)
		msg, err = newMockMsg().sign(badVal, signer.DomainStateReceiver)
		require.NoError(t, err)

		msg.From = vals.GetValidator("1").Address().String()
		require.Error(t, s.saveVote(msg))
	})

	t.Run("Sender votes", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		msg := newMockMsg()
		val1signed, err := msg.sign(vals.GetValidator("1"), signer.DomainStateReceiver)
		require.NoError(t, err)

		val2signed, err := msg.sign(vals.GetValidator("2"), signer.DomainStateReceiver)
		require.NoError(t, err)

		// vote with validator 1
		require.NoError(t, s.saveVote(val1signed))

		votes, err := s.state.BridgeMessageStore.getMessageVotes(0, msg.hash, 0)
		require.NoError(t, err)
		require.Len(t, votes, 1)

		// vote with validator 1 again (the votes do not increase)
		require.NoError(t, s.saveVote(val1signed))
		votes, _ = s.state.BridgeMessageStore.getMessageVotes(0, msg.hash, 0)
		require.Len(t, votes, 1)

		// vote with validator 2
		require.NoError(t, s.saveVote(val2signed))
		votes, _ = s.state.BridgeMessageStore.getMessageVotes(0, msg.hash, 0)
		require.Len(t, votes, 2)
	})
}

func TestBridgeEventManager_BuildCommitment(t *testing.T) {
	vals := validator.NewTestValidators(t, 5)

	s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
	s.validatorSet = vals.ToValidatorSet()

	// commitment is empty
	commitment, err := s.GetVotedBridgeBatch(1)
	require.NoError(t, err)
	require.Nil(t, commitment)

	s.pendingBridgeBatches = []*PendingBridgeBatch{
		{
			BridgeMessageBatch: &contractsapi.BridgeMessageBatch{
				SourceChainID:      big.NewInt(1),
				DestinationChainID: big.NewInt(0),
			},
		},
	}

	hash, err := s.pendingBridgeBatches[0].Hash()
	require.NoError(t, err)

	msg := newMockMsg().WithHash(hash.Bytes())

	// validators 0 and 1 vote for the proposal, there is not enough
	// voting power for the proposal
	signedMsg1, err := msg.sign(vals.GetValidator("0"), signer.DomainStateReceiver)
	require.NoError(t, err)

	signedMsg1.DestinationChainID = 0

	signedMsg2, err := msg.sign(vals.GetValidator("1"), signer.DomainStateReceiver)
	require.NoError(t, err)

	signedMsg2.DestinationChainID = 0

	require.NoError(t, s.saveVote(signedMsg1))
	require.NoError(t, s.saveVote(signedMsg2))

	commitment, err = s.GetVotedBridgeBatch(1)
	require.NoError(t, err) // there is no error if quorum is not met, since its a valid case
	require.Nil(t, commitment)

	// validator 2 and 3 vote for the proposal, there is enough voting power now

	signedMsg1, err = msg.sign(vals.GetValidator("2"), signer.DomainStateReceiver)
	require.NoError(t, err)

	signedMsg2, err = msg.sign(vals.GetValidator("3"), signer.DomainStateReceiver)
	require.NoError(t, err)

	require.NoError(t, s.saveVote(signedMsg1))
	require.NoError(t, s.saveVote(signedMsg2))

	commitment, err = s.GetVotedBridgeBatch(1)
	require.NoError(t, err)
	require.NotNil(t, commitment)
}

func TestBridgeEventManager_BuildProofs(t *testing.T) {
	vals := validator.NewTestValidators(t, 5)

	s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})

	for _, evnt := range generateBridgeMessageEvents(t, 20, 0) {
		require.NoError(t, s.state.BridgeMessageStore.insertBridgeMessageEvent(evnt))
	}

	require.NoError(t, s.buildBridgeBatch(nil, 0))
	require.Len(t, s.pendingBridgeBatches, 1)

	blsKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data, err := s.pendingBridgeBatches[0].BridgeMessageBatch.EncodeAbi()
	require.NoError(t, err)

	signature, err := blsKey.Sign(data, domain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	mockMsg := &BridgeBatchSigned{
		MessageBatch: &contractsapi.BridgeMessageBatch{
			Messages:           s.pendingBridgeBatches[0].BridgeMessageBatch.Messages,
			SourceChainID:      s.pendingBridgeBatches[0].BridgeMessageBatch.SourceChainID,
			DestinationChainID: s.pendingBridgeBatches[0].BridgeMessageBatch.DestinationChainID,
		},
		AggSignature: Signature{AggregatedSignature: aggSig},
		PublicKeys:   [][]byte{blsKey.PublicKey().Marshal()},
	}

	txData, err := mockMsg.EncodeAbi()
	require.NoError(t, err)

	tx := createStateTransactionWithData(types.Address{}, txData)

	req := &PostBlockRequest{
		FullBlock: &types.FullBlock{
			Block: &types.Block{
				Transactions: []*types.Transaction{tx},
			},
		},
	}

	length := len(mockMsg.MessageBatch.Messages)

	require.NoError(t, s.PostBlock(req))
	require.Equal(t, mockMsg.MessageBatch.Messages[length-1].ID.Uint64()+1, s.nextBridgeEventIdIndex[0])
}

func TestBridgeEventManager_RemoveProcessedEventsAndProofs(t *testing.T) {
	t.Skip()
	const stateSyncEventsCount = 5

	vals := validator.NewTestValidators(t, 5)

	s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
	stateSyncEvents := generateBridgeMessageEvents(t, stateSyncEventsCount, 0)

	for _, event := range stateSyncEvents {
		require.NoError(t, s.state.BridgeMessageStore.insertBridgeMessageEvent(event))
	}

	stateSyncEventsBefore, err := s.state.BridgeMessageStore.list()
	require.NoError(t, err)
	require.Equal(t, stateSyncEventsCount, len(stateSyncEventsBefore))

	for _, event := range stateSyncEvents {
		eventLog := createTestLogForStateSyncResultEvent(t, event.ID.Uint64())
		require.NoError(t, s.ProcessLog(&types.Header{Number: 10}, convertLog(eventLog), nil))
	}

	// all state sync events and their proofs should be removed from the store
	stateSyncEventsAfter, err := s.state.BridgeMessageStore.list()
	require.NoError(t, err)
	require.Equal(t, 0, len(stateSyncEventsAfter))
}

func TestBridgeEventManager_AddLog_BuildBridgeBatches(t *testing.T) {
	t.Parallel()

	vals := validator.NewTestValidators(t, 5)

	t.Run("Node is a validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})

		// empty log which is not an state sync
		require.NoError(t, s.AddLog(&ethgo.Log{}))
		bridgeEvents, err := s.state.BridgeMessageStore.list()

		require.NoError(t, err)
		require.Len(t, bridgeEvents, 0)

		var bridgeMessageEvent contractsapi.BridgeMsgEvent

		bridgeMessageEventID := bridgeMessageEvent.Sig()

		// log with the state sync topic but incorrect content
		require.Error(t, s.AddLog(&ethgo.Log{Topics: []ethgo.Hash{bridgeMessageEventID}}))
		bridgeEvents, err = s.state.BridgeMessageStore.list()

		require.NoError(t, err)
		require.Len(t, bridgeEvents, 0)

		// correct event log
		data, err := abi.MustNewType("tuple(string a)").Encode([]string{"data"})
		require.NoError(t, err)

		goodLog := &ethgo.Log{
			Topics: []ethgo.Hash{
				bridgeMessageEventID,
				ethgo.BytesToHash([]byte{0x0}), // state sync index 0
				ethgo.ZeroHash,
				ethgo.ZeroHash,
			},
			Data: data,
		}

		require.NoError(t, s.AddLog(goodLog))

		bridgeEvents, err = s.state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(0, 0, nil, 0)
		require.NoError(t, err)
		require.Len(t, bridgeEvents, 1)
		require.Len(t, s.pendingBridgeBatches, 1)
		length := len(s.pendingBridgeBatches[0].BridgeMessageBatch.Messages)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[length-1].ID.Uint64())

		// add one more log to have a minimum commitment
		goodLog2 := goodLog.Copy()
		goodLog2.Topics[1] = ethgo.BytesToHash([]byte{0x1}) // state sync index 1
		require.NoError(t, s.AddLog(goodLog2))

		require.Len(t, s.pendingBridgeBatches, 2)
		length = len(s.pendingBridgeBatches[1].BridgeMessageBatch.Messages)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[1].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(1), s.pendingBridgeBatches[1].BridgeMessageBatch.Messages[length-1].ID.Uint64())

		// add two more logs to have larger commitments
		goodLog3 := goodLog.Copy()
		goodLog3.Topics[1] = ethgo.BytesToHash([]byte{0x2}) // state sync index 2
		require.NoError(t, s.AddLog(goodLog3))

		goodLog4 := goodLog.Copy()
		goodLog4.Topics[1] = ethgo.BytesToHash([]byte{0x3}) // state sync index 3
		require.NoError(t, s.AddLog(goodLog4))

		length = len(s.pendingBridgeBatches[3].BridgeMessageBatch.Messages)

		require.Len(t, s.pendingBridgeBatches, 4)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[3].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(3), s.pendingBridgeBatches[3].BridgeMessageBatch.Messages[length-1].ID.Uint64())
	})

	t.Run("Node is not a validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeEventManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: false})

		// correct event log
		data, err := abi.MustNewType("tuple(string a)").Encode([]string{"data"})
		require.NoError(t, err)

		var bridgeMessageEvent contractsapi.BridgeMsgEvent

		goodLog := &ethgo.Log{
			Topics: []ethgo.Hash{
				bridgeMessageEvent.Sig(),
				ethgo.BytesToHash([]byte{0x0}), // state sync index 0
				ethgo.ZeroHash,
				ethgo.ZeroHash,
			},
			Data: data,
		}

		require.NoError(t, s.AddLog(goodLog))

		// node should have inserted given state sync event, but it shouldn't build any commitment
		bridgeMessages, err := s.state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(0, 0, nil, 0)
		require.NoError(t, err)
		require.Len(t, bridgeMessages, 1)
		require.Equal(t, uint64(0), bridgeMessages[0].ID.Uint64())
		require.Len(t, s.pendingBridgeBatches, 0)
	})
}

func createTestLogForStateSyncResultEvent(t *testing.T, stateSyncEventID uint64) *types.Log {
	t.Helper()

	var stateSyncResultEvent contractsapi.BridgeMessageResultEvent

	topics := make([]types.Hash, 3)
	topics[0] = types.Hash(stateSyncResultEvent.Sig())
	topics[1] = types.BytesToHash(common.EncodeUint64ToBytes(stateSyncEventID))
	topics[2] = types.BytesToHash(common.EncodeUint64ToBytes(1)) // Status = true
	someType := abi.MustNewType("tuple(string field1, string field2)")
	encodedData, err := someType.Encode(map[string]string{"field1": "value1", "field2": "value2"})
	require.NoError(t, err)

	return &types.Log{
		Address: contracts.StateReceiverContract,
		Topics:  topics,
		Data:    encodedData,
	}
}

type mockTopic struct {
	published proto.Message
}

func (m *mockTopic) consume() proto.Message {
	msg := m.published

	if m.published != nil {
		m.published = nil
	}

	return msg
}

func (m *mockTopic) Publish(obj proto.Message) error {
	m.published = obj

	return nil
}

func (m *mockTopic) Subscribe(handler func(obj interface{}, from peer.ID)) error {
	return nil
}

type mockMsg struct {
	hash  []byte
	epoch uint64
}

func newMockMsg() *mockMsg {
	hash := make([]byte, 32)
	rand.Read(hash)

	return &mockMsg{hash: hash}
}

func (m *mockMsg) WithHash(hash []byte) *mockMsg {
	m.hash = hash

	return m
}

func (m *mockMsg) sign(val *validator.TestValidator, domain []byte) (*TransportMessage, error) {
	signature, err := val.MustSign(m.hash, domain).Marshal()
	if err != nil {
		return nil, err
	}

	return &TransportMessage{
		BatchByte:   m.hash,
		Signature:   signature,
		From:        val.Address().String(),
		EpochNumber: m.epoch,
	}, nil
}

type mockRuntime struct {
	isActiveValidator bool
}

func (m *mockRuntime) IsActiveValidator() bool {
	return m.isActiveValidator
}
