package runner

import (
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo/wallet"
)

func TestMnemonic(t *testing.T) {
	realAddress := types.StringToAddress("0x85da99c8a7c2c95964c8efd687e95e632fc533d6")

	key, _ := wallet.NewWalletFromMnemonic("code code code code code code code code code code code quality")
	raw, _ := key.MarshallPrivateKey()

	ecdsaKey, _ := crypto.NewECDSAKeyFromRawPrivECDSA(raw)
	require.Equal(t, realAddress, ecdsaKey.Address())
}

func TestLoadRunner(t *testing.T) {
	t.Skip("this is only added for the sake of the example and running it in local")

	cfg := LoadTestConfig{
		Mnemonnic:       "code code code code code code code code code code code quality",
		LoadTestType:    "erc20",
		LoadTestName:    "test",
		JSONRPCUrl:      "http://localhost:10002",
		VUs:             10,
		TxsPerUser:      100,
		ReceiptsTimeout: 30 * time.Second,
		TxPoolTimeout:   30 * time.Minute,
	}

	runner := &LoadTestRunner{}

	require.NoError(t, runner.Run(cfg))
}
