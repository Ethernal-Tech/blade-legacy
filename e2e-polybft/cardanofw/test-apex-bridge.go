package cardanofw

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"path"
	"testing"
	"time"

	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

func SetupAndRunApexBridge(
	t *testing.T,
	ctx context.Context,
	dataDir string,
	bladeValidatorsNum int,
	bladeEpochSize int,
	primeNetworkAddress string,
	primeNetworkMagic int,
	primeOgmiosURL string,
	vectorNetworkAddress string,
	vectorNetworkMagic int,
	vectorOgmiosURL string,
) (*TestCardanoBridge, func()) {
	t.Helper()

	cleanupDataDir := func() {
		os.RemoveAll(dataDir)
	}

	cleanupDataDir()

	cb := NewTestCardanoBridge(dataDir, bladeValidatorsNum)

	require.NoError(t, cb.CardanoCreateWalletsAndAddresses(primeNetworkMagic, vectorNetworkMagic))

	fmt.Printf("Wallets and addresses created\n")

	txProviderPrime := wallet.NewOgmiosProvider(primeOgmiosURL)
	txProviderVector := wallet.NewOgmiosProvider(vectorOgmiosURL)

	primeGenesisWallet, err := GetGenesisWalletFromCluster(path.Join(path.Dir(dataDir), "cluster-1"), 1)
	require.NoError(t, err)

	sendAmount := uint64(10_000_000)
	require.NoError(t, SendTx(ctx, txProviderPrime, primeGenesisWallet, sendAmount,
		cb.PrimeMultisigAddr, primeNetworkMagic, []byte{}))

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 12, time.Second*10)
	require.NoError(t, err)

	fmt.Printf("Prime multisig addr funded\n")

	require.NoError(t, SendTx(ctx, txProviderPrime, primeGenesisWallet, sendAmount,
		cb.PrimeMultisigFeeAddr, primeNetworkMagic, []byte{}))

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigFeeAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 12, time.Second*10)
	require.NoError(t, err)

	fmt.Printf("Prime multisig fee addr funded\n")

	vectorGenesisWallet, err := GetGenesisWalletFromCluster(path.Join(path.Dir(dataDir), "cluster-2"), 1)
	require.NoError(t, err)

	require.NoError(t, SendTx(ctx, txProviderVector, vectorGenesisWallet, sendAmount,
		cb.VectorMultisigAddr, vectorNetworkMagic, []byte{}))

	err = wallet.WaitForAmount(context.Background(), txProviderVector, cb.VectorMultisigAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 12, time.Second*10)
	require.NoError(t, err)

	fmt.Printf("Vector multisig addr funded\n")

	require.NoError(t, SendTx(ctx, txProviderVector, vectorGenesisWallet, sendAmount,
		cb.VectorMultisigFeeAddr, vectorNetworkMagic, []byte{}))

	err = wallet.WaitForAmount(context.Background(), txProviderVector, cb.VectorMultisigFeeAddr, func(val *big.Int) bool {
		return val.Cmp(new(big.Int).SetUint64(sendAmount)) == 0
	}, 12, time.Second*10)
	require.NoError(t, err)

	fmt.Printf("Vector multisig fee addr funded\n")

	cb.StartValidators(t, bladeEpochSize)

	fmt.Printf("Validators started\n")

	cb.WaitForValidatorsReady(t)

	fmt.Printf("Validators ready\n")

	// need params for it to work properly
	primeTokenSupply := big.NewInt(int64(sendAmount))
	vectorTokenSupply := big.NewInt(int64(sendAmount))
	require.NoError(t, cb.RegisterChains(
		primeTokenSupply,
		primeOgmiosURL,
		vectorTokenSupply,
		vectorOgmiosURL,
	))

	fmt.Printf("Chain registered\n")

	// need params for it to work properly
	require.NoError(t, cb.GenerateConfigs(
		primeNetworkAddress,
		primeNetworkMagic,
		primeOgmiosURL,
		vectorNetworkAddress,
		vectorNetworkMagic,
		vectorOgmiosURL,
		40000,
		"test_api_key",
	))

	fmt.Printf("Configs generated\n")

	require.NoError(t, cb.StartValidatorComponents(ctx))
	fmt.Printf("Validator components started\n")

	require.NoError(t, cb.StartRelayer(ctx))
	fmt.Printf("Relayer started\n")

	return cb, func() {
		// cleanupDataDir()
		cb.StopValidators()
	}
}
