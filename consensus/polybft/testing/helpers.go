package testing

import (
	"crypto/rand"
	"testing"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
)

type TestHeadersMap struct {
	HeadersByNumber map[uint64]*types.Header
}

func (t *TestHeadersMap) AddHeader(header *types.Header) {
	if t.HeadersByNumber == nil {
		t.HeadersByNumber = map[uint64]*types.Header{}
	}

	t.HeadersByNumber[header.Number] = header
}

func (t *TestHeadersMap) GetHeader(number uint64) *types.Header {
	return t.HeadersByNumber[number]
}

func (t *TestHeadersMap) GetHeaderByHash(hash types.Hash) *types.Header {
	for _, header := range t.HeadersByNumber {
		if header.Hash == hash {
			return header
		}
	}

	return nil
}

func (t *TestHeadersMap) GetHeaders() []*types.Header {
	headers := make([]*types.Header, 0, len(t.HeadersByNumber))
	for _, header := range t.HeadersByNumber {
		headers = append(headers, header)
	}

	return headers
}

// GenerateRandomBytes generates byte array with random data of 32 bytes length
func GenerateRandomBytes(t *testing.T) (result []byte) {
	t.Helper()

	result = make([]byte, types.HashLength)
	_, err := rand.Reader.Read(result)
	require.NoError(t, err, "Cannot generate random byte array content.")

	return
}

func CreateTestKey(t *testing.T) *wallet.Key {
	t.Helper()

	return wallet.NewKey(GenerateTestAccount(t))
}

func CreateRandomTestKeys(t *testing.T, numberOfKeys int) []*wallet.Key {
	t.Helper()

	result := make([]*wallet.Key, numberOfKeys)

	for i := 0; i < numberOfKeys; i++ {
		result[i] = wallet.NewKey(GenerateTestAccount(t))
	}

	return result
}

func GenerateTestAccount(tb testing.TB) *wallet.Account {
	tb.Helper()

	acc, err := wallet.GenerateAccount()
	require.NoError(tb, err)

	return acc
}
