package bridge

import (
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path"
	"testing"
	"time"

	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/abi"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"

	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
)

var bigZero = big.NewInt(0)

func newTestState(t *testing.T) *BridgeManagerStore {
	t.Helper()

	dir := fmt.Sprintf("/tmp/consensus-temp_%v", time.Now().UTC().Format(time.RFC3339Nano))
	err := os.Mkdir(dir, 0775)

	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	})

	db, err := bolt.Open(path.Join(dir, "my.db"), 0666, nil)
	if err != nil {
		t.Fatal(err)
	}

	numOfBridges := uint64(1000)
	chainIds := make([]uint64, 0, numOfBridges)

	for i := uint64(0); i < numOfBridges; i++ {
		chainIds = append(chainIds, i)
	}

	store, err := newBridgeManagerStore(db, nil, chainIds)
	if err != nil {
		t.Fatal(err)
	}

	return store
}

func newTestBridgeManager(t *testing.T, key *validator.TestValidator, runtime Runtime) *bridgeEventManager {
	t.Helper()

	state := newTestState(t)
	require.NoError(t, state.insertEpoch(0, nil, 1))

	topic := &mockTopic{}

	s := newBridgeManager(
		hclog.NewNullLogger(),
		state,
		&bridgeEventManagerConfig{
			topic:             topic,
			key:               key.Key(),
			maxNumberOfEvents: maxNumberOfBatchEvents,
		}, runtime, 1, 0)

	return s
}

func TestBridgeEventManager_PostEpoch_BuildBridgeBatch(t *testing.T) {
	t.Parallel()

	vals := validator.NewTestValidators(t, 5)

	t.Run("When node is validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})

		// there are no bridge messages
		require.NoError(t, s.buildExternalBridgeBatch(nil))
		require.Nil(t, s.pendingBridgeBatches)

		bridgeMessages10 := generateBridgeMessageEvents(t, 10, 0)

		// add 5 bridge messages starting in index 0, it will generate one smaller batch
		for i := 0; i < 5; i++ {
			require.NoError(t, s.state.insertBridgeMessageEvent(bridgeMessages10[i]))
		}

		require.NoError(t, s.buildExternalBridgeBatch(nil))
		length := len(s.pendingBridgeBatches[0].BridgeMessageBatch.Messages)
		require.Len(t, s.pendingBridgeBatches, 1)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(4), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[length-1].ID.Uint64())
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].Epoch)

		// add the next 5 bridge messages, at that point, so that it generates a larger batch
		for i := 5; i < 10; i++ {
			require.NoError(t, s.state.insertBridgeMessageEvent(bridgeMessages10[i]))
		}

		require.NoError(t, s.buildExternalBridgeBatch(nil))
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

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: false})

		bridgeMessages10 := generateBridgeMessageEvents(t, 10, 0)

		// add 5 bridge messages starting in index 0, they will be saved to db
		for i := 0; i < 5; i++ {
			require.NoError(t, s.state.insertBridgeMessageEvent(bridgeMessages10[i]))
		}

		// I am not a validator so no batches should be built
		require.NoError(t, s.buildExternalBridgeBatch(nil))
		require.Len(t, s.pendingBridgeBatches, 0)
	})
}

func TestBridgeEventManager_MessagePool(t *testing.T) {
	t.Parallel()

	vals := validator.NewTestValidators(t, 5)

	t.Run("Old epoch", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})

		s.epoch = 1
		msg := &BridgeBatchVote{
			EpochNumber: 0,
		}

		err := s.saveVote(msg)
		require.NoError(t, err)
	})

	t.Run("Sender is not a validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		badVal := validator.NewTestValidator(t, "a", 0)
		msg, err := newMockMsg().sign(badVal, signer.DomainBridge)
		require.NoError(t, err)

		msg.SourceChainID = 1

		require.Error(t, s.saveVote(msg))
	})

	t.Run("Invalid epoch", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		val := newMockMsg()
		msg, err := val.sign(vals.GetValidator("0"), signer.DomainBridge)
		require.NoError(t, err)

		// invalid epoch +2
		msg.EpochNumber = 2

		require.NoError(t, s.saveVote(msg))

		// no votes for the current epoch
		votes, err := s.state.getMessageVotes(0, msg.Hash, 1)
		require.NoError(t, err)
		require.Len(t, votes, 0)

		// returns an error for the invalid epoch
		_, err = s.state.getMessageVotes(1, msg.Hash, 0)
		require.Error(t, err)
	})

	t.Run("Sender and signature mismatch", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		// validator signs the msg in behalf of another validator
		val := newMockMsg()
		msg, err := val.sign(vals.GetValidator("0"), signer.DomainBridge)
		require.NoError(t, err)

		msg.SourceChainID = 1

		msg.Sender = vals.GetValidator("1").Address().String()
		require.Error(t, s.saveVote(msg))

		// non validator signs the msg in behalf of a validator
		badVal := validator.NewTestValidator(t, "a", 0)
		msg, err = newMockMsg().sign(badVal, signer.DomainBridge)
		require.NoError(t, err)

		msg.SourceChainID = 1

		msg.Sender = vals.GetValidator("1").Address().String()
		require.Error(t, s.saveVote(msg))
	})

	t.Run("Sender votes", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
		s.validatorSet = vals.ToValidatorSet()

		msg := newMockMsg()
		val1signed, err := msg.sign(vals.GetValidator("1"), signer.DomainBridge)
		require.NoError(t, err)

		val1signed.SourceChainID = 1

		val2signed, err := msg.sign(vals.GetValidator("2"), signer.DomainBridge)
		require.NoError(t, err)

		val2signed.SourceChainID = 1

		// vote with validator 1
		require.NoError(t, s.saveVote(val1signed))

		votes, err := s.state.getMessageVotes(0, msg.hash, 1)
		require.NoError(t, err)
		require.Len(t, votes, 1)

		// vote with validator 1 again (the votes do not increase)
		require.NoError(t, s.saveVote(val1signed))
		votes, _ = s.state.getMessageVotes(0, msg.hash, 1)
		require.Len(t, votes, 1)

		// vote with validator 2
		require.NoError(t, s.saveVote(val2signed))
		votes, _ = s.state.getMessageVotes(0, msg.hash, 1)
		require.Len(t, votes, 2)
	})
}

func TestBridgeEventManager_BuildBridgeBatch(t *testing.T) {
	vals := validator.NewTestValidators(t, 5)

	s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
	s.validatorSet = vals.ToValidatorSet()

	// batch is empty
	batch, err := s.BridgeBatch(1)
	require.NoError(t, err)
	require.Nil(t, batch)

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
	signedMsg1, err := msg.sign(vals.GetValidator("0"), signer.DomainBridge)
	require.NoError(t, err)

	signedMsg1.SourceChainID = 1

	signedMsg2, err := msg.sign(vals.GetValidator("1"), signer.DomainBridge)
	require.NoError(t, err)

	signedMsg2.SourceChainID = 1

	require.NoError(t, s.saveVote(signedMsg1))
	require.NoError(t, s.saveVote(signedMsg2))

	batch, err = s.BridgeBatch(0)
	require.NoError(t, err) // there is no error if quorum is not met, since its a valid case
	require.Nil(t, batch)

	// validator 2 and 3 vote for the proposal, there is enough voting power now

	signedMsg1, err = msg.sign(vals.GetValidator("2"), signer.DomainBridge)
	require.NoError(t, err)

	signedMsg1.SourceChainID = 1

	signedMsg2, err = msg.sign(vals.GetValidator("3"), signer.DomainBridge)
	require.NoError(t, err)

	signedMsg2.SourceChainID = 1

	require.NoError(t, s.saveVote(signedMsg1))
	require.NoError(t, s.saveVote(signedMsg2))

	batch, err = s.BridgeBatch(1)
	require.NoError(t, err)
	require.NotNil(t, batch)
}

func TestBridgeEventManager_RemoveProcessedEventsAndProofs(t *testing.T) {
	const bridgeMessageEventsCount = 5

	vals := validator.NewTestValidators(t, 5)

	s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})
	bridgeMessageEvents := generateBridgeMessageEvents(t, bridgeMessageEventsCount, 0)

	for _, event := range bridgeMessageEvents {
		require.NoError(t, s.state.insertBridgeMessageEvent(event))
	}

	bridgeMessageEventsBefore, err := s.state.list()
	require.NoError(t, err)
	require.Equal(t, bridgeMessageEventsCount, len(bridgeMessageEventsBefore))

	for _, event := range bridgeMessageEvents {
		eventLog := createTestLogForBridgeMessageResultEvent(t, event.ID.Uint64())
		require.NoError(t, s.ProcessLog(&types.Header{Number: 10}, eventLog, nil))
	}

	// all bridge message events and their proofs should be removed from the store
	stateSyncEventsAfter, err := s.state.list()
	require.NoError(t, err)
	require.Equal(t, 0, len(stateSyncEventsAfter))
}

func TestBridgeEventManager_AddLog_BuildBridgeBatches(t *testing.T) {
	t.Parallel()

	vals := validator.NewTestValidators(t, 5)

	t.Run("Node is a validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: true})

		bridgeMsg := &contractsapi.BridgeMsgEvent{ID: bigZero, SourceChainID: big.NewInt(1), DestinationChainID: bigZero}
		bridgeMsgData, err := bridgeMsg.Encode()
		require.NoError(t, err)

		// empty log which is not an bridge message
		require.NoError(t, s.AddLog(big.NewInt(1), &ethgo.Log{Data: bridgeMsgData}))
		bridgeEvents, err := s.state.list()

		require.NoError(t, err)
		require.Len(t, bridgeEvents, 0)

		// log with the bridge message topic but incorrect content
		require.Error(t, s.AddLog(big.NewInt(1), &ethgo.Log{Topics: []ethgo.Hash{bridgeMessageEventSig}, Data: bridgeMsgData}))
		bridgeEvents, err = s.state.list()

		require.NoError(t, err)
		require.Len(t, bridgeEvents, 0)

		// correct event log
		data, err := abi.MustNewType("tuple(uint256 a, string b, string c)").Encode([]string{"1", "data2", "data3"})
		require.NoError(t, err)

		goodLog := &ethgo.Log{
			Topics: []ethgo.Hash{
				bridgeMessageEventSig,
				ethgo.BytesToHash([]byte{0x0}), // bridge message index 0
				ethgo.ZeroHash,
				ethgo.ZeroHash,
			},
			Data: data,
		}

		require.NoError(t, s.AddLog(big.NewInt(1), goodLog))

		bridgeEvents, err = s.state.getBridgeMessageEventsForBridgeBatch(0, 0, nil, 1, 0)
		require.NoError(t, err)
		require.Len(t, bridgeEvents, 1)
		require.Len(t, s.pendingBridgeBatches, 1)
		length := len(s.pendingBridgeBatches[0].BridgeMessageBatch.Messages)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(0), s.pendingBridgeBatches[0].BridgeMessageBatch.Messages[length-1].ID.Uint64())

		// add one more log to have a minimum batch
		goodLog2 := goodLog.Copy()
		goodLog2.Topics[1] = ethgo.BytesToHash([]byte{0x1}) // bridgeMsg event index 1
		require.NoError(t, s.AddLog(big.NewInt(1), goodLog2))

		require.Len(t, s.pendingBridgeBatches, 2)
		length = len(s.pendingBridgeBatches[1].BridgeMessageBatch.Messages)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[1].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(1), s.pendingBridgeBatches[1].BridgeMessageBatch.Messages[length-1].ID.Uint64())

		// add two more logs to have larger batch
		goodLog3 := goodLog.Copy()
		goodLog3.Topics[1] = ethgo.BytesToHash([]byte{0x2}) // bridgeMsg event index 2
		require.NoError(t, s.AddLog(big.NewInt(1), goodLog3))

		goodLog4 := goodLog.Copy()
		goodLog4.Topics[1] = ethgo.BytesToHash([]byte{0x3}) // bridgeMsg event index 3
		require.NoError(t, s.AddLog(big.NewInt(1), goodLog4))

		length = len(s.pendingBridgeBatches[3].BridgeMessageBatch.Messages)

		require.Len(t, s.pendingBridgeBatches, 4)
		require.Equal(t, uint64(0), s.pendingBridgeBatches[3].BridgeMessageBatch.Messages[0].ID.Uint64())
		require.Equal(t, uint64(3), s.pendingBridgeBatches[3].BridgeMessageBatch.Messages[length-1].ID.Uint64())
	})

	t.Run("Node is not a validator", func(t *testing.T) {
		t.Parallel()

		s := newTestBridgeManager(t, vals.GetValidator("0"), &mockRuntime{isActiveValidator: false})

		// correct event log
		data, err := abi.MustNewType("tuple(uint256 a, string b, string c)").Encode([]string{"1", "data2", "data3"})
		require.NoError(t, err)

		var bridgeMessageEvent contractsapi.BridgeMsgEvent

		goodLog := &ethgo.Log{
			Topics: []ethgo.Hash{
				bridgeMessageEvent.Sig(),
				ethgo.BytesToHash([]byte{0x0}), // bridge message index 0
				ethgo.ZeroHash,
				ethgo.ZeroHash,
			},
			Data: data,
		}

		require.NoError(t, s.AddLog(big.NewInt(1), goodLog))

		// node should have inserted given bridgeMsg event, but it shouldn't build any batch
		bridgeMessages, err := s.state.getBridgeMessageEventsForBridgeBatch(0, 0, nil, 1, 0)
		require.NoError(t, err)
		require.Len(t, bridgeMessages, 1)
		require.Equal(t, uint64(0), bridgeMessages[0].ID.Uint64())
		require.Len(t, s.pendingBridgeBatches, 0)
	})
}

func createTestLogForBridgeMessageResultEvent(t *testing.T, bridgeMessageEventID uint64) *ethgo.Log {
	t.Helper()

	data, err := abi.MustNewType("tuple(uint256 a, string b, string c)").Encode([]string{"1", "data2", "data3"})
	require.NoError(t, err)

	return &ethgo.Log{
		Topics: []ethgo.Hash{
			bridgeMessageResultEventSig,
			ethgo.BytesToHash(common.EncodeUint64ToBytes(bridgeMessageEventID)),
			ethgo.BytesToHash(common.EncodeUint64ToBytes(1))},
		Data: data,
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

func (m *mockMsg) sign(val *validator.TestValidator, domain []byte) (*BridgeBatchVote, error) {
	signature, err := val.MustSign(m.hash, domain).Marshal()
	if err != nil {
		return nil, err
	}

	return &BridgeBatchVote{
		Hash: m.hash,
		BridgeBatchVoteConsensusData: &BridgeBatchVoteConsensusData{
			Signature: signature,
			Sender:    val.Address().String(),
		},
		EpochNumber: m.epoch,
	}, nil
}

type mockRuntime struct {
	isActiveValidator bool
}

func (m *mockRuntime) IsActiveValidator() bool {
	return m.isActiveValidator
}

var _ BridgeManager = (*mockBridgeManager)(nil)

type mockBridgeManager struct {
	chainID uint64
	state   *BridgeManagerStore
}

func (*mockBridgeManager) AddLog(chainID *big.Int, eventLog *ethgo.Log) error {
	return nil
}

func (*mockBridgeManager) Close() {}

func (*mockBridgeManager) GetLogFilters() map[types.Address][]types.Hash {
	return nil
}

// PostBlock implements BridgeManager.
func (*mockBridgeManager) PostBlock() error {
	return nil
}

// PostEpoch implements BridgeManager.
func (mbm *mockBridgeManager) PostEpoch(req *polytypes.PostEpochRequest) error {
	if err := mbm.state.insertEpoch(req.NewEpochID, req.DBTx, mbm.chainID); err != nil {
		return err
	}

	return nil
}

// ProcessLog implements BridgeManager.
func (*mockBridgeManager) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bolt.Tx) error {
	return nil
}

// Start implements BridgeManager.
func (*mockBridgeManager) Start(runtimeCfg *config.Runtime) error {
	return nil
}

func (mbm *mockBridgeManager) BuildExitEventRoot(epoch uint64) (types.Hash, error) {
	return types.ZeroHash, nil
}
func (mbm *mockBridgeManager) BridgeBatch(pendingBlockNumber uint64) (*BridgeBatchSigned, error) {
	return nil, nil
}