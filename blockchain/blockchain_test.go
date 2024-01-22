package blockchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/hashicorp/go-hclog"
	lru "github.com/hashicorp/golang-lru"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xPolygon/polygon-edge/blockchain/storage"
	"github.com/0xPolygon/polygon-edge/blockchain/storage/leveldb"
	"github.com/0xPolygon/polygon-edge/blockchain/storage/memory"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	B  = 1
	KB = 1024 * B
	MB = 1024 * KB
	GB = 1024 * MB
)

func TestGenesis(t *testing.T) {
	b := NewTestBlockchain(t, nil)

	// add genesis block
	genesis := &types.Header{Difficulty: 1, Number: 0}
	genesis.ComputeHash()

	assert.NoError(t, b.writeGenesisImpl(genesis))

	header := b.Header()
	assert.Equal(t, header.Hash, genesis.Hash)
}

type dummyChain struct {
	headers map[byte]*types.Header
}

func (c *dummyChain) add(h *header) error {
	if _, ok := c.headers[h.hash]; ok {
		return fmt.Errorf("hash already imported")
	}

	var parent types.Hash
	if h.number != 0 {
		p, ok := c.headers[h.parent]
		if !ok {
			return fmt.Errorf("parent not found %v", h.parent)
		}

		parent = p.Hash
	}

	hh := &types.Header{
		ParentHash: parent,
		Number:     h.number,
		Difficulty: h.diff,
		ExtraData:  []byte{h.hash},
	}

	hh.ComputeHash()
	c.headers[h.hash] = hh

	return nil
}

type header struct {
	hash   byte
	parent byte
	number uint64
	diff   uint64
}

func (h *header) Parent(parent byte) *header {
	h.parent = parent
	h.number = uint64(parent) + 1

	return h
}

func (h *header) Diff(d uint64) *header {
	h.diff = d

	return h
}

func (h *header) Number(d uint64) *header {
	h.number = d

	return h
}

func mock(number byte) *header {
	return &header{
		hash:   number,
		parent: number - 1,
		number: uint64(number),
		diff:   uint64(number),
	}
}

func TestInsertHeaders(t *testing.T) {
	type evnt struct {
		NewChain []*header
		OldChain []*header
		Diff     *big.Int
	}

	type headerEvnt struct {
		header *header
		event  *evnt
	}

	var cases = []struct {
		Name    string
		History []*headerEvnt
		Head    *header
		Forks   []*header
		Chain   []*header
		TD      uint64
	}{
		{
			Name: "Genesis",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
			},
			Head: mock(0x0),
			Chain: []*header{
				mock(0x0),
			},
			TD: 0,
		},
		{
			Name: "Linear",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(3),
					},
				},
			},
			Head: mock(0x2),
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x2),
			},
			TD: 0 + 1 + 2,
		},
		{
			Name: "Keep block with higher difficulty",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x3).Parent(0x1).Diff(5),
					event: &evnt{
						NewChain: []*header{
							mock(0x3).Parent(0x1).Diff(5),
						},
						Diff: big.NewInt(6),
					},
				},
				{
					// This block has lower difficulty than the current chain (fork)
					header: mock(0x2).Parent(0x1).Diff(3),
					event: &evnt{
						OldChain: []*header{
							mock(0x2).Parent(0x1).Diff(3),
						},
					},
				},
			},
			Head:  mock(0x3),
			Forks: []*header{mock(0x2)},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x3).Parent(0x1).Diff(5),
			},
			TD: 0 + 1 + 5,
		},
		{
			Name: "Reorg",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(1 + 2),
					},
				},
				{
					header: mock(0x3),
					event: &evnt{
						NewChain: []*header{
							mock(0x3),
						},
						Diff: big.NewInt(1 + 2 + 3),
					},
				},
				{
					// First reorg
					header: mock(0x4).Parent(0x1).Diff(10).Number(2),
					event: &evnt{
						// add block 4
						NewChain: []*header{
							mock(0x4).Parent(0x1).Diff(10).Number(2),
						},
						// remove block 2 and 3
						OldChain: []*header{
							mock(0x2),
							mock(0x3),
						},
						Diff: big.NewInt(1 + 10),
					},
				},
				{
					header: mock(0x5).Parent(0x4).Diff(11).Number(3),
					event: &evnt{
						NewChain: []*header{
							mock(0x5).Parent(0x4).Diff(11).Number(3),
						},
						Diff: big.NewInt(1 + 10 + 11),
					},
				},
				{
					header: mock(0x6).Parent(0x3).Number(4),
					event: &evnt{
						// lower difficulty, its a fork
						OldChain: []*header{
							mock(0x6).Parent(0x3).Number(4),
						},
					},
				},
			},
			Head:  mock(0x5),
			Forks: []*header{mock(0x6)},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x4).Parent(0x1).Diff(10).Number(2),
				mock(0x5).Parent(0x4).Diff(11).Number(3),
			},
			TD: 0 + 1 + 10 + 11,
		},
		{
			Name: "Forks in reorgs",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(1 + 2),
					},
				},
				{
					header: mock(0x3),
					event: &evnt{
						NewChain: []*header{
							mock(0x3),
						},
						Diff: big.NewInt(1 + 2 + 3),
					},
				},
				{
					// fork 1. 0x1 -> 0x2 -> 0x4
					header: mock(0x4).Parent(0x2).Diff(11),
					event: &evnt{
						NewChain: []*header{
							mock(0x4).Parent(0x2).Diff(11),
						},
						OldChain: []*header{
							mock(0x3),
						},
						Diff: big.NewInt(1 + 2 + 11),
					},
				},
				{
					// fork 2. 0x1 -> 0x2 -> 0x3 -> 0x5
					header: mock(0x5).Parent(0x3),
					event: &evnt{
						OldChain: []*header{
							mock(0x5).Parent(0x3),
						},
					},
				},
				{
					// fork 3. 0x1 -> 0x2 -> 0x6
					header: mock(0x6).Parent(0x2).Diff(5),
					event: &evnt{
						OldChain: []*header{
							mock(0x6).Parent(0x2).Diff(5),
						},
					},
				},
			},
			Head:  mock(0x4),
			Forks: []*header{mock(0x5), mock(0x6)},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x2),
				mock(0x4).Parent(0x2).Diff(11),
			},
			TD: 0 + 1 + 2 + 11,
		},
		{
			Name: "Head from old long fork",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(1 + 2),
					},
				},
				{
					// fork 1.
					header: mock(0x3).Parent(0x0).Diff(5),
					event: &evnt{
						NewChain: []*header{
							mock(0x3).Parent(0x0).Diff(5),
						},
						OldChain: []*header{
							mock(0x1),
							mock(0x2),
						},
						Diff: big.NewInt(0 + 5),
					},
				},
				{
					// Add back the 0x2 fork
					header: mock(0x4).Parent(0x2).Diff(10),
					event: &evnt{
						NewChain: []*header{
							mock(0x4).Parent(0x2).Diff(10),
							mock(0x2),
							mock(0x1),
						},
						OldChain: []*header{
							mock(0x3).Parent(0x0).Diff(5),
						},
						Diff: big.NewInt(1 + 2 + 10),
					},
				},
			},
			Head: mock(0x4).Parent(0x2).Diff(10),
			Forks: []*header{
				mock(0x2),
				mock(0x3).Parent(0x0).Diff(5),
			},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x2),
				mock(0x4).Parent(0x2).Diff(10),
			},
			TD: 0 + 1 + 2 + 10,
		},
	}

	for _, cc := range cases {
		t.Run(cc.Name, func(t *testing.T) {
			b := NewTestBlockchain(t, nil)

			chain := dummyChain{
				headers: map[byte]*types.Header{},
			}
			for _, i := range cc.History {
				if err := chain.add(i.header); err != nil {
					t.Fatal(err)
				}
			}

			checkEvents := func(a []*header, b []*types.Header) {
				if len(a) != len(b) {
					t.Fatal("bad size")
				}
				for indx := range a {
					if chain.headers[a[indx].hash].Hash != b[indx].Hash {
						t.Fatal("bad")
					}
				}
			}

			// genesis is 0x0
			if err := b.writeGenesisImpl(chain.headers[0x0]); err != nil {
				t.Fatal(err)
			}

			// we need to subscribe just after the genesis and history
			sub := b.SubscribeEvents()

			// run the history
			for i := 1; i < len(cc.History); i++ {
				headers := []*types.Header{chain.headers[cc.History[i].header.hash]}
				if err := b.WriteHeadersWithBodies(headers); err != nil {
					t.Fatal(err)
				}

				// get the event
				evnt := sub.GetEvent()
				checkEvents(cc.History[i].event.NewChain, evnt.NewChain)
				checkEvents(cc.History[i].event.OldChain, evnt.OldChain)

				if evnt.Difficulty != nil {
					if evnt.Difficulty.Cmp(cc.History[i].event.Diff) != 0 {
						t.Fatal("bad diff in event")
					}
				}
			}

			head := b.Header()

			expected, ok := chain.headers[cc.Head.hash]
			assert.True(t, ok)

			// check that we got the right hash
			assert.Equal(t, head.Hash, expected.Hash)

			forks, err := b.GetForks()
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				t.Fatal(err)
			}

			expectedForks := []types.Hash{}

			for _, i := range cc.Forks {
				expectedForks = append(expectedForks, chain.headers[i.hash].Hash)
			}

			if len(forks) != 0 {
				if len(forks) != len(expectedForks) {
					t.Fatalf("forks length dont match, expected %d but found %d", len(expectedForks), len(forks))
				} else {
					if !reflect.DeepEqual(forks, expectedForks) {
						t.Fatal("forks dont match")
					}
				}
			}

			// Check chain of forks
			if cc.Chain != nil {
				for indx, i := range cc.Chain {
					block, _ := b.GetBlockByNumber(uint64(indx), true)
					if block.Hash().String() != chain.headers[i.hash].Hash.String() {
						t.Fatal("bad")
					}
				}
			}

			if td, _ := b.GetChainTD(); cc.TD != td.Uint64() {
				t.Fatal("bad")
			}
		})
	}
}

func TestForkUnknownParents(t *testing.T) {
	b := NewTestBlockchain(t, nil)

	h0 := NewTestHeaders(10)
	h1 := AppendNewTestHeaders(h0[:5], 10)

	// Write genesis
	batchWriter := storage.NewBatchWriter(b.db)
	td := new(big.Int).SetUint64(h0[0].Difficulty)

	batchWriter.PutCanonicalHeader(h0[0], td)

	assert.NoError(t, b.writeBatchAndUpdate(batchWriter, h0[0], td, true))

	// Write 10 headers
	assert.NoError(t, b.WriteHeadersWithBodies(h0[1:]))

	// Cannot write this header because the father h1[11] is not known
	assert.Error(t, b.WriteHeadersWithBodies([]*types.Header{h1[12]}))
}

func TestBlockchainWriteBody(t *testing.T) {
	t.Parallel()

	var (
		addr = types.StringToAddress("1")
	)

	newChain := func(
		t *testing.T,
		txFromByTxHash map[types.Hash]types.Address,
		path string,
	) *Blockchain {
		t.Helper()

		dbStorage, err := memory.NewMemoryStorage(nil)
		assert.NoError(t, err)

		chain := &Blockchain{
			db: dbStorage,
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		return chain
	}

	t.Run("should succeed if tx has from field", func(t *testing.T) {
		t.Parallel()

		tx := &types.Transaction{
			Value: big.NewInt(10),
			V:     big.NewInt(1),
			From:  addr,
		}

		block := &types.Block{
			Header: &types.Header{},
			Transactions: []*types.Transaction{
				tx,
			},
		}

		tx.ComputeHash()
		block.Header.ComputeHash()

		txFromByTxHash := map[types.Hash]types.Address{}

		chain := newChain(t, txFromByTxHash, "t1")
		defer chain.db.Close()
		batchWriter := storage.NewBatchWriter(chain.db)

		assert.NoError(
			t,
			chain.writeBody(batchWriter, block),
		)
		assert.NoError(t, batchWriter.WriteBatch())
	})

	t.Run("should return error if tx doesn't have from and recovering address fails", func(t *testing.T) {
		t.Parallel()

		tx := &types.Transaction{
			Value: big.NewInt(10),
			V:     big.NewInt(1),
		}

		block := &types.Block{
			Header: &types.Header{},
			Transactions: []*types.Transaction{
				tx,
			},
		}

		tx.ComputeHash()
		block.Header.ComputeHash()

		txFromByTxHash := map[types.Hash]types.Address{}

		chain := newChain(t, txFromByTxHash, "t2")
		defer chain.db.Close()
		batchWriter := storage.NewBatchWriter(chain.db)

		assert.ErrorIs(
			t,
			errRecoveryAddressFailed,
			chain.writeBody(batchWriter, block),
		)
		assert.NoError(t, batchWriter.WriteBatch())
	})

	t.Run("should recover from address and store to storage", func(t *testing.T) {
		t.Parallel()

		tx := &types.Transaction{
			Value: big.NewInt(10),
			V:     big.NewInt(1),
		}

		block := &types.Block{
			Header: &types.Header{},
			Transactions: []*types.Transaction{
				tx,
			},
		}

		tx.ComputeHash()
		block.Header.ComputeHash()

		txFromByTxHash := map[types.Hash]types.Address{
			tx.Hash: addr,
		}

		chain := newChain(t, txFromByTxHash, "t3")
		defer chain.db.Close()
		batchWriter := storage.NewBatchWriter(chain.db)

		batchWriter.PutHeader(block.Header)

		assert.NoError(t, chain.writeBody(batchWriter, block))

		assert.NoError(t, batchWriter.WriteBatch())

		readBody, ok := chain.readBody(block.Hash())
		assert.True(t, ok)

		assert.Equal(t, addr, readBody.Transactions[0].From)
	})
}

func Test_recoverFromFieldsInBlock(t *testing.T) {
	t.Parallel()

	var (
		addr1 = types.StringToAddress("1")
		addr2 = types.StringToAddress("1")
		addr3 = types.StringToAddress("1")
	)

	computeTxHashes := func(txs ...*types.Transaction) {
		for _, tx := range txs {
			tx.ComputeHash()
		}
	}

	t.Run("should succeed", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: addr1}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2)

		txFromByTxHash[tx2.Hash] = addr2

		block := &types.Block{
			Transactions: []*types.Transaction{
				tx1,
				tx2,
			},
		}

		assert.NoError(
			t,
			chain.recoverFromFieldsInBlock(block),
		)
	})

	t.Run("should stop and return error if recovery fails", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: types.ZeroAddress}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}
		tx3 := &types.Transaction{Nonce: 2, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2, tx3)

		// returns only addresses for tx1 and tx3
		txFromByTxHash[tx1.Hash] = addr1
		txFromByTxHash[tx3.Hash] = addr3

		block := &types.Block{
			Transactions: []*types.Transaction{
				tx1,
				tx2,
				tx3,
			},
		}

		assert.ErrorIs(
			t,
			chain.recoverFromFieldsInBlock(block),
			errRecoveryAddressFailed,
		)

		assert.Equal(t, addr1, tx1.From)
		assert.Equal(t, types.ZeroAddress, tx2.From)
		assert.Equal(t, types.ZeroAddress, tx3.From)
	})
}

func Test_recoverFromFieldsInTransactions(t *testing.T) {
	t.Parallel()

	var (
		addr1 = types.StringToAddress("1")
		addr2 = types.StringToAddress("1")
		addr3 = types.StringToAddress("1")
	)

	computeTxHashes := func(txs ...*types.Transaction) {
		for _, tx := range txs {
			tx.ComputeHash()
		}
	}

	t.Run("should succeed", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			logger: hclog.NewNullLogger(),
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: addr1}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2)

		txFromByTxHash[tx2.Hash] = addr2

		transactions := []*types.Transaction{
			tx1,
			tx2,
		}

		assert.True(
			t,
			chain.recoverFromFieldsInTransactions(transactions),
		)
	})

	t.Run("should succeed even though recovery fails for some transactions", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			logger: hclog.NewNullLogger(),
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: types.ZeroAddress}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}
		tx3 := &types.Transaction{Nonce: 2, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2, tx3)

		// returns only addresses for tx1 and tx3
		txFromByTxHash[tx1.Hash] = addr1
		txFromByTxHash[tx3.Hash] = addr3

		transactions := []*types.Transaction{
			tx1,
			tx2,
			tx3,
		}

		assert.True(t, chain.recoverFromFieldsInTransactions(transactions))

		assert.Equal(t, addr1, tx1.From)
		assert.Equal(t, types.ZeroAddress, tx2.From)
		assert.Equal(t, addr3, tx3.From)
	})

	t.Run("should return false if all transactions has from field", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			logger: hclog.NewNullLogger(),
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: addr1}
		tx2 := &types.Transaction{Nonce: 1, From: addr2}

		computeTxHashes(tx1, tx2)

		txFromByTxHash[tx2.Hash] = addr2

		transactions := []*types.Transaction{
			tx1,
			tx2,
		}

		assert.False(
			t,
			chain.recoverFromFieldsInTransactions(transactions),
		)
	})
}

func TestBlockchainReadBody(t *testing.T) {
	dbStorage, err := memory.NewMemoryStorage(nil)
	assert.NoError(t, err)

	txFromByTxHash := make(map[types.Hash]types.Address)
	addr := types.StringToAddress("1")

	b := &Blockchain{
		logger: hclog.NewNullLogger(),
		db:     dbStorage,
		txSigner: &mockSigner{
			txFromByTxHash: txFromByTxHash,
		},
	}

	batchWriter := storage.NewBatchWriter(b.db)

	tx := &types.Transaction{
		Value: big.NewInt(10),
		V:     big.NewInt(1),
	}

	tx.ComputeHash()

	block := &types.Block{
		Header: &types.Header{},
		Transactions: []*types.Transaction{
			tx,
		},
	}

	block.Header.ComputeHash()

	txFromByTxHash[tx.Hash] = types.ZeroAddress

	batchWriter.PutCanonicalHeader(block.Header, big.NewInt(0))

	require.NoError(t, b.writeBody(batchWriter, block))

	assert.NoError(t, batchWriter.WriteBatch())

	txFromByTxHash[tx.Hash] = addr

	readBody, found := b.readBody(block.Hash())

	assert.True(t, found)
	assert.Equal(t, addr, readBody.Transactions[0].From)
}

func TestCalculateGasLimit(t *testing.T) {
	tests := []struct {
		name             string
		blockGasTarget   uint64
		parentGasLimit   uint64
		expectedGasLimit uint64
	}{
		{
			name:             "should increase next gas limit towards target",
			blockGasTarget:   25000000,
			parentGasLimit:   20000000,
			expectedGasLimit: 20000000/1024 + 20000000,
		},
		{
			name:             "should decrease next gas limit towards target",
			blockGasTarget:   25000000,
			parentGasLimit:   26000000,
			expectedGasLimit: 26000000 - 26000000/1024,
		},
		{
			name:             "should not alter gas limit when exactly the same",
			blockGasTarget:   25000000,
			parentGasLimit:   25000000,
			expectedGasLimit: 25000000,
		},
		{
			name:             "should increase to the exact gas target if adding the delta surpasses it",
			blockGasTarget:   25000000 + 25000000/1024 - 100, // - 100 so that it takes less than the delta to reach it
			parentGasLimit:   25000000,
			expectedGasLimit: 25000000 + 25000000/1024 - 100,
		},
		{
			name:             "should decrease to the exact gas target if subtracting the delta surpasses it",
			blockGasTarget:   25000000 - 25000000/1024 + 100, // + 100 so that it takes less than the delta to reach it
			parentGasLimit:   25000000,
			expectedGasLimit: 25000000 - 25000000/1024 + 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageCallback := func(storage *storage.MockStorage) {
				storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
					return &types.Header{
						// This is going to be the parent block header
						GasLimit: tt.parentGasLimit,
					}, nil
				})
			}

			b, blockchainErr := NewMockBlockchain(map[TestCallbackType]interface{}{
				StorageCallback: storageCallback,
			})
			if blockchainErr != nil {
				t.Fatalf("unable to construct the blockchain, %v", blockchainErr)
			}

			b.genesisConfig.Params = &chain.Params{
				BlockGasTarget: tt.blockGasTarget,
			}

			nextGas, err := b.CalculateGasLimit(1)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedGasLimit, nextGas)
		})
	}
}

// TestGasPriceAverage tests the average gas price of the
// blockchain
func TestGasPriceAverage(t *testing.T) {
	testTable := []struct {
		name               string
		previousAverage    *big.Int
		previousCount      *big.Int
		newValues          []*big.Int
		expectedNewAverage *big.Int
	}{
		{
			"no previous average data",
			big.NewInt(0),
			big.NewInt(0),
			[]*big.Int{
				big.NewInt(1),
				big.NewInt(2),
				big.NewInt(3),
				big.NewInt(4),
				big.NewInt(5),
			},
			big.NewInt(3),
		},
		{
			"previous average data",
			// For example (5 + 5 + 5 + 5 + 5) / 5
			big.NewInt(5),
			big.NewInt(5),
			[]*big.Int{
				big.NewInt(1),
				big.NewInt(2),
				big.NewInt(3),
			},
			// (5 * 5 + 1 + 2 + 3) / 8
			big.NewInt(3),
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			// Setup the mock data
			blockchain := NewTestBlockchain(t, nil)
			blockchain.gpAverage.price = testCase.previousAverage
			blockchain.gpAverage.count = testCase.previousCount

			// Update the average gas price
			blockchain.updateGasPriceAvg(testCase.newValues)

			// Make sure the average gas price count is correct
			assert.Equal(
				t,
				int64(len(testCase.newValues))+testCase.previousCount.Int64(),
				blockchain.gpAverage.count.Int64(),
			)

			// Make sure the average gas price is correct
			assert.Equal(t, testCase.expectedNewAverage.String(), blockchain.gpAverage.price.String())
		})
	}
}

// TestBlockchain_VerifyBlockParent verifies that parent block verification
// errors are handled correctly
func TestBlockchain_VerifyBlockParent(t *testing.T) {
	t.Parallel()

	emptyHeader := &types.Header{
		Hash:       types.ZeroHash,
		ParentHash: types.ZeroHash,
	}
	emptyHeader.ComputeHash()

	t.Run("Missing parent block", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return nil, errors.New("not found")
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block
		block := &types.Block{
			Header: &types.Header{
				ParentHash: types.ZeroHash,
			},
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrParentNotFound)
	})

	t.Run("Parent hash mismatch", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block whose parent hash will
		// not match the computed parent hash
		block := &types.Block{
			Header: emptyHeader.Copy(),
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrParentHashMismatch)
	})

	t.Run("Invalid block sequence", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block with a number much higher than the parent
		block := &types.Block{
			Header: &types.Header{
				Number: 10,
			},
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrParentHashMismatch)
	})

	t.Run("Invalid block sequence", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block with a number much higher than the parent
		block := &types.Block{
			Header: &types.Header{
				Number:     10,
				ParentHash: emptyHeader.Copy().Hash,
			},
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrInvalidBlockSequence)
	})

	t.Run("Invalid block gas limit", func(t *testing.T) {
		t.Parallel()

		parentHeader := emptyHeader.Copy()
		parentHeader.GasLimit = 5000

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block with a number much higher than the parent
		block := &types.Block{
			Header: &types.Header{
				Number:     1,
				ParentHash: parentHeader.Hash,
				GasLimit:   parentHeader.GasLimit + 1000, // The gas limit is greater than the allowed rate
			},
		}

		assert.Error(t, blockchain.verifyBlockParent(block))
	})
}

// TestBlockchain_VerifyBlockBody makes sure that the block body is verified correctly
func TestBlockchain_VerifyBlockBody(t *testing.T) {
	t.Parallel()

	emptyHeader := &types.Header{
		Hash:       types.ZeroHash,
		ParentHash: types.ZeroHash,
	}

	t.Run("Invalid SHA3 Uncles root", func(t *testing.T) {
		t.Parallel()

		blockchain, err := NewMockBlockchain(nil)
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.ZeroHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, ErrInvalidSha3Uncles)
	})

	t.Run("Invalid Transactions root", func(t *testing.T) {
		t.Parallel()

		blockchain, err := NewMockBlockchain(nil)
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, ErrInvalidTxRoot)
	})

	t.Run("Invalid execution result - missing parent", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return nil, errors.New("not found")
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
				TxRoot:     types.EmptyRootHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, ErrParentNotFound)
	})

	t.Run("Invalid execution result - unable to fetch block creator", func(t *testing.T) {
		t.Parallel()

		errBlockCreatorNotFound := errors.New("not found")

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			// This is used for parent fetching
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		// Set up the verifier callback
		verifierCallback := func(verifier *MockVerifier) {
			// This is used for error-ing out on the block creator fetch
			verifier.HookGetBlockCreator(func(t *types.Header) (types.Address, error) {
				return types.ZeroAddress, errBlockCreatorNotFound
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback:  storageCallback,
			VerifierCallback: verifierCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
				TxRoot:     types.EmptyRootHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, errBlockCreatorNotFound)
	})

	t.Run("Invalid execution result - unable to execute transactions", func(t *testing.T) {
		t.Parallel()

		errUnableToExecute := errors.New("unable to execute transactions")

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			// This is used for parent fetching
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		executorCallback := func(executor *mockExecutor) {
			// This is executor processing
			executor.HookProcessBlock(func(
				hash types.Hash,
				block *types.Block,
				address types.Address,
			) (*state.Transition, error) {
				return nil, errUnableToExecute
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback:  storageCallback,
			ExecutorCallback: executorCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
				TxRoot:     types.EmptyRootHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, errUnableToExecute)
	})
}

func TestBlockchain_CalculateBaseFee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		blockNumber          uint64
		parentBaseFee        uint64
		parentGasLimit       uint64
		parentGasUsed        uint64
		elasticityMultiplier uint64
		forks                *chain.Forks
		getLatestConfigFn    getChainConfigDelegate
		expectedBaseFee      uint64
	}{
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      chain.GenesisBaseFee,
		}, // usage == target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1125000000,
		}, // usage == target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        9000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      987500000,
		}, // usage below target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        9000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1100000000,
		}, // usage below target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        11000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      1012500000,
		}, // usage above target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        11000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1150000000,
		}, // usage above target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        20000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      1125000000,
		}, // usage full
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        20000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1375000000,
		}, // usage full
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        0,
			elasticityMultiplier: 2,
			expectedBaseFee:      875000000,
		}, // usage 0
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        0,
			elasticityMultiplier: 4,
			expectedBaseFee:      875000000,
		}, // usage 0
		{
			blockNumber:     6,
			forks:           &chain.Forks{chain.London: chain.NewFork(10)},
			expectedBaseFee: 0,
		}, // London hard fork disabled
		{
			blockNumber:     6,
			parentBaseFee:   0,
			expectedBaseFee: 10,
		},
		// first block with London hard fork
		// (return base fee value configured in the genesis)
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 4,
			forks:                chain.AllForksEnabled,
			getLatestConfigFn: func() (*chain.Params, error) {
				return &chain.Params{BaseFeeChangeDenom: 4}, nil
			},
			expectedBaseFee: 1250000000,
		}, // governance hard fork enabled
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 4,
			forks:                chain.AllForksEnabled,
			getLatestConfigFn: func() (*chain.Params, error) {
				return nil, errors.New("failed to retrieve chain config")
			},
			expectedBaseFee: 1000000008,
		}, // governance hard fork enabled
	}

	for i, test := range tests {
		test := test

		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			t.Parallel()

			forks := &chain.Forks{
				chain.London: chain.NewFork(5),
			}

			if test.forks != nil {
				forks = test.forks
			}

			blockchain := &Blockchain{
				logger: hclog.NewNullLogger(),
				genesisConfig: &chain.Chain{
					Params: &chain.Params{
						Forks:              forks,
						BaseFeeChangeDenom: chain.BaseFeeChangeDenom,
						BaseFeeEM:          test.elasticityMultiplier,
					},
					Genesis: &chain.Genesis{
						BaseFee: 10,
					},
				},
			}

			blockchain.setCurrentHeader(&types.Header{
				Number:   test.blockNumber + 1,
				GasLimit: test.parentGasLimit,
				GasUsed:  test.parentGasUsed,
				BaseFee:  test.parentBaseFee,
			}, big.NewInt(1))

			blockchain.SetConsensus(&MockVerifier{getChainConfigFn: test.getLatestConfigFn})

			parent := &types.Header{
				Number:   test.blockNumber,
				GasLimit: test.parentGasLimit,
				GasUsed:  test.parentGasUsed,
				BaseFee:  test.parentBaseFee,
			}

			got := blockchain.CalculateBaseFee(parent)
			assert.Equal(t, test.expectedBaseFee, got, fmt.Sprintf("expected %d, got %d", test.expectedBaseFee, got))
		})
	}
}

func TestBlockchain_WriteFullBlock(t *testing.T) {
	t.Parallel()

	getKey := func(p []byte, k []byte) []byte {
		return append(append(make([]byte, 0, len(p)+len(k)), p...), k...)
	}
	db := map[string][]byte{}
	consensusMock := &MockVerifier{
		processHeadersFn: func(hs []*types.Header) error {
			assert.Len(t, hs, 1)

			return nil
		},
	}

	storageMock := storage.NewMockStorage()
	storageMock.HookNewBatch(func() storage.Batch {
		return memory.NewBatchMemory(db)
	})

	bc := &Blockchain{
		gpAverage: &gasPriceAverage{
			count: new(big.Int),
		},
		logger:    hclog.NewNullLogger(),
		db:        storageMock,
		consensus: consensusMock,
		genesisConfig: &chain.Chain{
			Params: &chain.Params{
				Forks: &chain.Forks{
					chain.London: chain.NewFork(5),
				},
				BaseFeeEM: 4,
			},
			Genesis: &chain.Genesis{},
		},
		stream: newEventStream(),
	}

	bc.headersCache, _ = lru.New(10)
	bc.difficultyCache, _ = lru.New(10)

	existingTD := big.NewInt(1)
	existingHeader := &types.Header{Number: 1}
	header := &types.Header{
		Number: 2,
	}
	receipts := []*types.Receipt{
		{GasUsed: 100},
		{GasUsed: 200},
	}
	tx := &types.Transaction{
		Value: big.NewInt(1),
	}

	tx.ComputeHash()
	header.ComputeHash()
	existingHeader.ComputeHash()
	bc.currentHeader.Store(existingHeader)
	bc.currentDifficulty.Store(existingTD)

	header.ParentHash = existingHeader.Hash
	bc.txSigner = &mockSigner{
		txFromByTxHash: map[types.Hash]types.Address{
			tx.Hash: {1, 2},
		},
	}

	// already existing block write
	err := bc.WriteFullBlock(&types.FullBlock{
		Block: &types.Block{
			Header:       existingHeader,
			Transactions: []*types.Transaction{tx},
		},
		Receipts: receipts,
	}, "polybft")

	require.NoError(t, err)
	require.Equal(t, 0, len(db))
	require.Equal(t, uint64(1), bc.currentHeader.Load().Number)

	// already existing block write
	err = bc.WriteFullBlock(&types.FullBlock{
		Block: &types.Block{
			Header:       header,
			Transactions: []*types.Transaction{tx},
		},
		Receipts: receipts,
	}, "polybft")

	require.NoError(t, err)
	require.Equal(t, 8, len(db))
	require.Equal(t, uint64(2), bc.currentHeader.Load().Number)
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.BODY, header.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.TX_LOOKUP_PREFIX, tx.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.HEADER, header.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.HEAD, storage.HASH))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.CANONICAL, common.EncodeUint64ToBytes(header.Number)))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.DIFFICULTY, header.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.CANONICAL, common.EncodeUint64ToBytes(header.Number)))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.RECEIPTS, header.Hash.Bytes()))])
}

func TestDiskUsageWriteBatchAndUpdate(t *testing.T) {
	const (
		checkInterval  = 10000 //milliseconds
		numberOfBlocks = 100
		blockTime      = 2000 //milliseconds
	)

	jsonFile, err := os.Open("testblock.json")
	require.NoError(t, err)

	byteValue, _ := io.ReadAll(jsonFile)

	receiptsJSONFile, err := os.Open("receipts.json")
	require.NoError(t, err)

	receiptsByteVaulue, _ := io.ReadAll(receiptsJSONFile)

	blockWriter(t, checkInterval, blockTime, numberOfBlocks, byteValue, receiptsByteVaulue)
}

func TestDiskUsageWriteBatchAndUpdateNoTimeout(t *testing.T) {
	const (
		checkInterval  = 15 // milliseconds
		numberOfBlocks = 100
		blockTime      = 0 //milliseconds
	)

	jsonFile, err := os.Open("testblock.json")
	require.NoError(t, err)

	byteValue, _ := io.ReadAll(jsonFile)

	receiptsJSONFile, err := os.Open("receipts.json")
	require.NoError(t, err)

	receiptsByteVaulue, _ := io.ReadAll(receiptsJSONFile)

	blockWriter(t, checkInterval, blockTime, numberOfBlocks, byteValue, receiptsByteVaulue)
}

func blockWriter(t *testing.T, checkInterval, blockTime, numberOfBlocks uint64, byteToRead []byte, receiptsBytesToRead []byte) {
	t.Helper()

	type blockCounter struct {
		mu sync.RWMutex
		x  uint64
	}

	counter := &blockCounter{x: 0}

	quitChan := make(chan bool)

	p, err := os.MkdirTemp("", "DiskUsageTest")
	require.NoError(t, err)

	db, err := leveldb.NewLevelDBStorage(
		filepath.Join(p),
		hclog.NewNullLogger(),
	)
	require.NoError(t, err)

	block, err := CustomJSONBlockUnmarshall(byteToRead)
	require.NoError(t, err)

	receipts, err := CustomReceiptsUnmarshall(receiptsBytesToRead)
	require.NoError(t, err)

	dirSizeCheck := func() {
		for {
			select {
			case <-quitChan:
				return
			default:
				dirSizeValue, err := DirSize(p)

				if err != nil {
					t.Log(err)
				}

				counter.mu.RLock()

				t.Logf("BLOCK: %d DIRSIZE IS: %d and average is:%.2f", counter.x, dirSizeValue, float64(dirSizeValue)/float64(counter.x))

				counter.mu.RUnlock()

				time.Sleep(time.Millisecond * time.Duration(checkInterval))
			}
		}
	}

	blockchain := &Blockchain{db: db}

	require.NoError(t, os.MkdirAll(p, 0755))

	require.NoError(t, err)

	dirSizeBeforeBlocks, err := DirSize(p)
	require.NoError(t, err)
	t.Logf("DIRSIZE IS: %d", dirSizeBeforeBlocks)

	go dirSizeCheck()

	for i := 0; i < int(numberOfBlocks); i++ {
		batchWriter := storage.NewBatchWriter(db)
		block.Block.Header.Number = uint64(i)

		batchWriter.PutHeader(block.Block.Header)
		batchWriter.PutBody(block.Block.Hash(), block.Block.Body())

		batchWriter.PutReceipts(block.Block.Hash(), receipts)

		require.NoError(t, blockchain.writeBatchAndUpdate(batchWriter, block.Block.Header, big.NewInt(0), false))

		counter.mu.Lock()

		counter.x++

		counter.mu.Unlock()

		time.Sleep(time.Millisecond * time.Duration(blockTime))
	}
	quitChan <- true

	dirSizeAfterBlocks, err := DirSize(p)
	require.NoError(t, err)
	t.Logf("DIRSIZE After All blocks: %d, average blocks size: %.2f", dirSizeAfterBlocks, float64(dirSizeAfterBlocks)/float64(numberOfBlocks))

	db.Close()

	assert.NotEqual(t, dirSizeBeforeBlocks, dirSizeAfterBlocks)
}

func CustomJSONBlockUnmarshall(jsonData []byte) (*types.FullBlock, error) {
	var ( //nolint:prealloc
		dat          map[string]interface{}
		err          error
		transactions []*types.Transaction
	)

	if err = json.Unmarshal(jsonData, &dat); err != nil {
		return nil, err
	}

	header := &types.Header{}

	header.ParentHash = types.StringToHash(dat["parentHash"].(string))             //nolint:forcetypeassert
	header.Sha3Uncles = types.StringToHash(dat["parentHash"].(string))             //nolint:forcetypeassert
	header.StateRoot = types.StringToHash(dat["stateRoot"].(string))               //nolint:forcetypeassert
	header.ReceiptsRoot = types.StringToHash(dat["receiptsRoot"].(string))         //nolint:forcetypeassert
	header.LogsBloom = types.Bloom(types.StringToBytes(dat["logsBloom"].(string))) //nolint:forcetypeassert

	difficulty := dat["difficulty"].(string) //nolint:forcetypeassert

	header.Difficulty, err = common.ParseUint64orHex(&difficulty)
	if err != nil {
		return nil, err
	}

	number := dat["number"].(string) //nolint:forcetypeassert

	header.Number, err = common.ParseUint64orHex(&number)
	if err != nil {
		return nil, err
	}

	gasLimit := dat["gasLimit"].(string) //nolint:forcetypeassert

	header.GasLimit, err = common.ParseUint64orHex(&gasLimit)
	if err != nil {
		return nil, err
	}

	gasUsed := dat["gasUsed"].(string) //nolint:forcetypeassert

	header.GasUsed, err = common.ParseUint64orHex(&gasUsed)
	if err != nil {
		return nil, err
	}

	timestamp := dat["timestamp"].(string) //nolint:forcetypeassert

	header.Timestamp, err = common.ParseUint64orHex(&timestamp)
	if err != nil {
		return nil, err
	}

	header.ExtraData = types.StringToBytes(dat["extraData"].(string)) //nolint:forcetypeassert

	nonce := dat["nonce"].(string) //nolint:forcetypeassert

	nonceNumber, err := common.ParseUint64orHex(&nonce)
	if err != nil {
		return nil, err
	}

	header.Nonce = types.Nonce{byte(nonceNumber)}

	header.Hash = types.StringToHash(dat["hash"].(string)) //nolint:forcetypeassert

	for _, transactionJSON := range dat["transactions"].([]interface{}) { //nolint:forcetypeassert
		tr := transactionJSON.(map[string]interface{}) //nolint:forcetypeassert
		transaction := &types.Transaction{}
		transaction.Hash = types.StringToHash(tr["hash"].(string)) //nolint:forcetypeassert
		nonce := tr["nonce"].(string)                              //nolint:forcetypeassert

		nonceNumber, err := common.ParseUint64orHex(&nonce)
		if err != nil {
			return nil, err
		}

		transaction.Nonce = nonceNumber

		transaction.From = types.StringToAddress(tr["from"].(string)) //nolint:forcetypeassert
		addr := types.StringToAddress(tr["to"].(string))              //nolint:forcetypeassert
		transaction.To = &addr

		value := tr["value"].(string) //nolint:forcetypeassert

		valueNumber, err := common.ParseUint256orHex(&value)
		if err != nil {
			return nil, err
		}

		transaction.Value = valueNumber

		gasPrice := tr["gasPrice"].(string) //nolint:forcetypeassert

		gasPriceNumber, err := common.ParseUint256orHex(&gasPrice)
		if err != nil {
			return nil, err
		}

		transaction.GasPrice = gasPriceNumber

		transaction.Input = []byte(tr["input"].(string)) //nolint:forcetypeassert

		v := tr["v"].(string) //nolint:forcetypeassert

		vNumber, err := common.ParseUint256orHex(&v)
		if err != nil {
			return nil, err
		}

		transaction.V = vNumber

		r := tr["r"].(string) //nolint:forcetypeassert

		rNumber, err := common.ParseUint256orHex(&r)
		if err != nil {
			return nil, err
		}

		transaction.R = rNumber

		s := tr["s"].(string) //nolint:forcetypeassert

		sNumber, err := common.ParseUint256orHex(&s)
		if err != nil {
			return nil, err
		}

		transaction.S = sNumber

		chainID := tr["chainId"].(string) //nolint:forcetypeassert

		chainIDNumber, err := common.ParseUint256orHex(&chainID)
		if err != nil {
			return nil, err
		}

		transaction.ChainID = chainIDNumber

		txType := tr["type"].(string) //nolint:forcetypeassert

		txTypeNumber, err := common.ParseUint64orHex(&txType)
		if err != nil {
			return nil, err
		}

		transaction.Type = types.TxType(txTypeNumber)

		transactions = append(transactions, transaction)
	}

	return &types.FullBlock{Block: &types.Block{Header: header, Transactions: transactions}}, nil
}

func CustomReceiptsUnmarshall(jsonData []byte) ([]*types.Receipt, error) {
	var ( //nolint:prealloc
		dat      map[string]interface{}
		err      error
		receipts []*types.Receipt
	)

	if err = json.Unmarshal(jsonData, &dat); err != nil {
		return nil, err
	}

	for _, receiptInterface := range dat["result"].([]interface{}) { //nolint:forcetypeassert
		receipt := &types.Receipt{}
		receiptJSON := receiptInterface.(map[string]interface{}) //nolint:forcetypeassert

		receipt.TxHash = types.StringToHash(receiptJSON["transactionHash"].(string)) //nolint:forcetypeassert

		if receiptJSON["contractAddress"] != nil {
			addr := types.StringToAddress(receiptJSON["contractAddress"].(string)) //nolint:forcetypeassert
			receipt.ContractAddress = &addr
		}

		cumulativeGasUsed := receiptJSON["cumulativeGasUsed"].(string) //nolint:forcetypeassert

		receipt.CumulativeGasUsed, err = common.ParseUint64orHex(&cumulativeGasUsed)
		if err != nil {
			return nil, err
		}

		gasUsed := receiptJSON["gasUsed"].(string) //nolint:forcetypeassert

		receipt.GasUsed, err = common.ParseUint64orHex(&gasUsed)
		if err != nil {
			return nil, err
		}

		receipt.LogsBloom = types.Bloom(types.StringToBytes(receiptJSON["logsBloom"].(string))) //nolint:forcetypeassert

		status := receiptJSON["status"].(string) //nolint:forcetypeassert

		statusNumber, err := common.ParseUint64orHex(&status)
		if err != nil {
			return nil, err
		}

		if statusNumber == 1 {
			success := types.ReceiptSuccess
			receipt.Status = &success
		} else {
			failed := types.ReceiptFailed
			receipt.Status = &failed
		}

		var logs []*types.Log

		for _, logInterface := range receiptJSON["logs"].([]interface{}) { //nolint:forcetypeassert
			log := &types.Log{}
			logJSON := logInterface.(map[string]interface{}) //nolint:forcetypeassert

			log.Address = types.StringToAddress(logJSON["address"].(string)) //nolint:forcetypeassert

			if logJSON["topics"] != nil {
				for _, topic := range logJSON["topics"].([]interface{}) { //nolint:forcetypeassert
					log.Topics = append(log.Topics, types.StringToHash(topic.(string))) //nolint:forcetypeassert
				}
			}

			log.Data = types.StringToBytes(logJSON["data"].(string)) //nolint:forcetypeassert

			logs = append(logs, log)
		}

		receipts = append(receipts, receipt)
	}

	return receipts, nil
}

func DirSize(path string) (uint64, error) {
	var size uint64

	entries, err := os.ReadDir(path)
	if err != nil {
		return size, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDirSize, err := DirSize(path + "/" + entry.Name())

			if err != nil {
				log.Printf("failed to calculate size of directory %s: %v\n", entry.Name(), err)
			}

			size += subDirSize
		} else {
			fileInfo, err := entry.Info()

			if err != nil {
				log.Printf("failed to get info of file %s: %v\n", entry.Name(), err)

				continue
			}
			size += uint64(fileInfo.Size())
		}
	}

	return size, nil
}
