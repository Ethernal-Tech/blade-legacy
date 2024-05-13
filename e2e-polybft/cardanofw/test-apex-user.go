package cardanofw

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path"
	"testing"
	"time"

	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

type TestApexUser struct {
	basePath string

	primeNetworkMagic  uint
	vectorNetworkMagic uint
	PrimeWallet        wallet.IWallet
	VectorWallet       wallet.IWallet
	PrimeAddress       string
	VectorAddress      string
}

func NewTestApexUser(t *testing.T, primeNetworkMagic, vectorNetworkMagic uint) *TestApexUser {
	t.Helper()

	basePath, err := os.MkdirTemp("/tmp", "apex-user")
	require.NoError(t, err)

	primeWallet, err := wallet.NewStakeWalletManager().Create(
		path.Join(basePath, "prime"), true)
	require.NoError(t, err)

	vectorWallet, err := wallet.NewStakeWalletManager().Create(
		path.Join(basePath, "vector"), true)
	require.NoError(t, err)

	primeUserAddress, _, err := wallet.GetWalletAddress(primeWallet, primeNetworkMagic)
	require.NoError(t, err)

	vectorUserAddress, _, err := wallet.GetWalletAddress(vectorWallet, vectorNetworkMagic)
	require.NoError(t, err)

	return &TestApexUser{
		basePath:           basePath,
		PrimeWallet:        primeWallet,
		VectorWallet:       vectorWallet,
		PrimeAddress:       primeUserAddress,
		VectorAddress:      vectorUserAddress,
		primeNetworkMagic:  primeNetworkMagic,
		vectorNetworkMagic: vectorNetworkMagic,
	}
}

func (u *TestApexUser) SendToUser(
	t *testing.T,
	ctx context.Context, txProvider wallet.ITxProvider,
	sender wallet.IWallet, sendAmount uint64,
	isPrime bool,
) {
	t.Helper()

	networkMagic := u.primeNetworkMagic
	addr := u.PrimeAddress

	if !isPrime {
		networkMagic = u.vectorNetworkMagic
		addr = u.VectorAddress
	}

	utxos, err := txProvider.GetUtxos(ctx, addr)
	require.NoError(t, err)

	prevAmount := wallet.GetUtxosSum(utxos)

	_, err = SendTx(ctx, txProvider, sender,
		sendAmount, addr, int(networkMagic), []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(
		context.Background(), txProvider, u.PrimeAddress, func(val *big.Int) bool {
			return val.Cmp(prevAmount) > 0
		}, 60, time.Second*2)
	require.NoError(t, err)
}

func (u *TestApexUser) BridgeAmount(
	t *testing.T, ctx context.Context,
	txProvider wallet.ITxProvider,
	multisigAddr, feeAddr string, sendAmount uint64, isPrime bool,
) string {
	t.Helper()

	const feeAmount = 1_100_000

	networkMagic := u.primeNetworkMagic
	sender := u.PrimeWallet
	senderAddr := u.PrimeAddress
	receiverAddr := u.VectorAddress

	if !isPrime {
		networkMagic = u.vectorNetworkMagic
		sender = u.VectorWallet
		senderAddr = u.VectorAddress
		receiverAddr = u.PrimeAddress
	}

	utxos, err := txProvider.GetUtxos(ctx, multisigAddr)
	require.NoError(t, err)

	prevAmount := wallet.GetUtxosSum(utxos)

	var receivers = map[string]uint64{
		receiverAddr: sendAmount,
		feeAddr:      feeAmount,
	}

	bridgingRequestMetadata, err := CreateMetaData(senderAddr, receivers)
	require.NoError(t, err)

	txHash, err := SendTx(ctx, txProvider, sender,
		sendAmount+feeAmount, multisigAddr, int(networkMagic), bridgingRequestMetadata)
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProvider, multisigAddr,
		func(val *big.Int) bool {
			return val.Cmp(prevAmount) > 0
		}, 60, time.Second*2)
	require.NoError(t, err)

	return txHash
}

func (u *TestApexUser) Dispose() {
	_ = os.RemoveAll(u.basePath)
	_ = os.Remove(u.basePath)
}

func CreateMetaData(sender string, receivers map[string]uint64) ([]byte, error) {
	type BridgingRequestMetadataTransaction struct {
		Address []string `cbor:"a" json:"a"`
		Amount  uint64   `cbor:"m" json:"m"`
	}

	var transactions = make([]BridgingRequestMetadataTransaction, 0, len(receivers))
	for addr, amount := range receivers {
		transactions = append(transactions, BridgingRequestMetadataTransaction{
			Address: SplitString(addr, 40),
			Amount:  amount,
		})
	}

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"t":  "bridge",
			"d":  "vector",
			"s":  SplitString(sender, 40),
			"tx": transactions,
		},
	}

	return json.Marshal(metadata)
}
