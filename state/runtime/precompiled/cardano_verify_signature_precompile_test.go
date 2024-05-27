package precompiled

import (
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/types"
	cardano_wallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo/abi"
)

var (
	protocolParameters, _ = hex.DecodeString("7b22636f6c6c61746572616c50657263656e74616765223a3135302c22646563656e7472616c697a6174696f6e223a6e756c6c2c22657865637574696f6e556e6974507269636573223a7b2270726963654d656d6f7279223a302e303537372c2270726963655374657073223a302e303030303732317d2c2265787472615072616f73456e74726f7079223a6e756c6c2c226d6178426c6f636b426f647953697a65223a39303131322c226d6178426c6f636b457865637574696f6e556e697473223a7b226d656d6f7279223a36323030303030302c227374657073223a32303030303030303030307d2c226d6178426c6f636b48656164657253697a65223a313130302c226d6178436f6c6c61746572616c496e70757473223a332c226d61785478457865637574696f6e556e697473223a7b226d656d6f7279223a31343030303030302c227374657073223a31303030303030303030307d2c226d6178547853697a65223a31363338342c226d617856616c756553697a65223a353030302c226d696e506f6f6c436f7374223a3137303030303030302c226d696e5554784f56616c7565223a6e756c6c2c226d6f6e6574617279457870616e73696f6e223a302e3030332c22706f6f6c506c65646765496e666c75656e6365223a302e332c22706f6f6c5265746972654d617845706f6368223a31382c2270726f746f636f6c56657273696f6e223a7b226d616a6f72223a382c226d696e6f72223a307d2c227374616b65416464726573734465706f736974223a323030303030302c227374616b65506f6f6c4465706f736974223a3530303030303030302c227374616b65506f6f6c5461726765744e756d223a3530302c227472656173757279437574223a302e322c2274784665654669786564223a3135353338312c22747846656550657242797465223a34342c227574786f436f737450657242797465223a343331307d")
)

func Test_cardanoVerifySignaturePrecompile_ValidSignature(t *testing.T) {
	txRaw, txHash := createTx(t)
	walletBasic, walletFee := createWallets(t)

	witness, err := cardano_wallet.CreateTxWitness(txHash, walletBasic)
	require.NoError(t, err)

	witnessFee, err := cardano_wallet.CreateTxWitness(txHash, walletFee)
	require.NoError(t, err)

	prec := &cardanoVerifySignaturePrecompile{}

	// with witnesses
	value, err := prec.run(
		encodeCardanoVerifySignature(t, txRaw, witness, walletBasic.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolTrue, value)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, txRaw, witnessFee, walletFee.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolTrue, value)

	// with signatures
	signature, _, err := cardano_wallet.TxWitnessRaw(witness).GetSignatureAndVKey()
	require.NoError(t, err)

	signatureFee, _, err := cardano_wallet.TxWitnessRaw(witnessFee).GetSignatureAndVKey()
	require.NoError(t, err)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, txRaw, signature, walletBasic.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolTrue, value)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, txRaw, signatureFee, walletFee.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolTrue, value)

	keyHash, err := cardano_wallet.GetKeyHash(walletBasic.GetVerificationKey())
	require.NoError(t, err)

	// message
	message := []byte(fmt.Sprintf("Hello world! My keyHash is: %s", keyHash))
	signature, err = cardano_wallet.SignMessage(
		walletBasic.GetSigningKey(), walletBasic.GetVerificationKey(), message)
	require.NoError(t, err)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, string(message), signature, walletBasic.GetVerificationKey(), false),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolTrue, value)
}

func Test_cardanoVerifySignaturePrecompile_InvalidSignature(t *testing.T) {
	txRaw, txHash := createTx(t)
	walletBasic, walletFee := createWallets(t)

	witness, err := cardano_wallet.CreateTxWitness(txHash, walletBasic)
	require.NoError(t, err)

	witnessFee, err := cardano_wallet.CreateTxWitness(txHash, walletFee)
	require.NoError(t, err)

	prec := &cardanoVerifySignaturePrecompile{}

	// with witnesses
	value, err := prec.run(
		encodeCardanoVerifySignature(t, txRaw, witness, walletFee.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolFalse, value)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, txRaw, witnessFee, walletBasic.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolFalse, value)

	// with signatures
	signature, _, err := cardano_wallet.TxWitnessRaw(witness).GetSignatureAndVKey()
	require.NoError(t, err)

	signatureFee, _, err := cardano_wallet.TxWitnessRaw(witnessFee).GetSignatureAndVKey()
	require.NoError(t, err)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, txRaw, signature, walletFee.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolFalse, value)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, txRaw, signatureFee, walletBasic.GetVerificationKey(), true),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolFalse, value)

	keyHash, err := cardano_wallet.GetKeyHash(walletBasic.GetVerificationKey())
	require.NoError(t, err)

	// message
	message := []byte(fmt.Sprintf("Hello world! My keyHash is: %s", keyHash))
	signature, err = cardano_wallet.SignMessage(
		walletBasic.GetSigningKey(), walletBasic.GetVerificationKey(), message)
	require.NoError(t, err)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, string(message), signature, walletFee.GetVerificationKey(), false),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolFalse, value)

	value, err = prec.run(
		encodeCardanoVerifySignature(t, string(message)+"0", signature, walletBasic.GetVerificationKey(), false),
		types.ZeroAddress, nil)
	require.NoError(t, err)
	require.Equal(t, abiBoolFalse, value)
}

func Test_cardanoVerifySignaturePrecompile_InvalidInputs(t *testing.T) {
	prec := &cardanoVerifySignaturePrecompile{}

	_, err := prec.run(
		[]byte{1, 2, 3},
		types.ZeroAddress, nil)
	require.Error(t, err)

	txRaw, _ := createTx(t)

	_, err = prec.run(
		encodeCardanoVerifySignature(t, txRaw, []byte{}, []byte{}, true),
		types.ZeroAddress, nil)
	require.Error(t, err)
}

func encodeCardanoVerifySignature(t *testing.T,
	txRaw string, witnessOrSignature []byte, verificationKey []byte, isTx bool) []byte {
	t.Helper()

	encoded, err := abi.Encode([]interface{}{
		txRaw,
		hex.EncodeToString(witnessOrSignature),
		string(verificationKey),
		isTx,
	},
		cardanoVerifySignaturePrecompileInputABIType)
	require.NoError(t, err)

	return encoded
}

func createWallets(t *testing.T) (cardano_wallet.IWallet, cardano_wallet.IWallet) {
	t.Helper()

	walletDir, err := os.MkdirTemp("", "cardano-txs-wallets")
	require.NoError(t, err)

	defer func() {
		os.RemoveAll(walletDir)
		os.Remove(walletDir)
	}()

	walletMngr := cardano_wallet.NewWalletManager()

	walletBasic, err := walletMngr.Create(path.Join(walletDir, "wallet"), false)
	require.NoError(t, err)

	walletFee, err := walletMngr.Create(path.Join(walletDir, "wallet_fee"), false)
	require.NoError(t, err)

	return walletBasic, walletFee
}

func createTx(t *testing.T) (string, string) {
	t.Helper()

	const (
		testNetMagic = 203
		ttl          = uint64(28096)
	)

	walletsKeyHashes := []string{
		"d6b67f93ffa4e2651271cc9bcdbdedb2539911266b534d9c163cba21",
		"cba89c7084bf0ce4bf404346b668a7e83c8c9c250d1cafd8d8996e41",
		"79df3577e4c7d7da04872c2182b8d8829d7b477912dbf35d89287c39",
		"2368e8113bd5f32d713751791d29acee9e1b5a425b0454b963b2558b",
		"06b4c7f5254d6395b527ac3de60c1d77194df7431d85fe55ca8f107d",
	}
	walletsFeeKeyHashes := []string{
		"f0f4837b3a306752a2b3e52394168bc7391de3dce11364b723cc55cf",
		"47344d5bd7b2fea56336ba789579705a944760032585ef64084c92db",
		"f01018c1d8da54c2f557679243b09af1c4dd4d9c671512b01fa5f92b",
		"6837232854849427dae7c45892032d7ded136c5beb13c68fda635d87",
		"d215701e2eb17c741b9d306cba553f9fbaaca1e12a5925a065b90fa8",
	}

	policyScriptMultiSig, err := cardano_wallet.NewPolicyScript(walletsKeyHashes, len(walletsKeyHashes)*2/3+1)
	require.NoError(t, err)

	policyScriptFeeMultiSig, err := cardano_wallet.NewPolicyScript(walletsFeeKeyHashes, len(walletsFeeKeyHashes)*2/3+1)
	require.NoError(t, err)

	multiSigAddr, err := policyScriptMultiSig.CreateMultiSigAddress(testNetMagic)
	require.NoError(t, err)

	multiSigFeeAddr, err := policyScriptFeeMultiSig.CreateMultiSigAddress(testNetMagic)
	require.NoError(t, err)

	outputs := []cardano_wallet.TxOutput{
		{
			Addr:   "addr_test1vqjysa7p4mhu0l25qknwznvj0kghtr29ud7zp732ezwtzec0w8g3u",
			Amount: cardano_wallet.MinUTxODefaultValue,
		},
	}
	outputsSum := cardano_wallet.GetOutputsSum(outputs)

	builder, err := cardano_wallet.NewTxBuilder()
	require.NoError(t, err)

	defer builder.Dispose()

	multiSigInputs := cardano_wallet.TxInputs{
		Inputs: []cardano_wallet.TxInput{
			{
				Hash:  "e99a5bde15aa05f24fcc04b7eabc1520d3397283b1ee720de9fe2653abbb0c9f",
				Index: 0,
			},
			{
				Hash:  "d1fd0d772be7741d9bfaf0b037d02d2867a987ccba3e6ba2ee9aa2a861b73145",
				Index: 2,
			},
		},
		Sum: cardano_wallet.MinUTxODefaultValue*2 + 20,
	}

	multiSigFeeInputs := cardano_wallet.TxInputs{
		Inputs: []cardano_wallet.TxInput{
			{
				Hash:  "098236134e0f2077a6434dd9d7727126fa8b3627bcab3ae030a194d46eded73e",
				Index: 0,
			},
		},
		Sum: cardano_wallet.MinUTxODefaultValue * 2,
	}

	builder.SetTimeToLive(ttl).SetProtocolParameters(protocolParameters)
	builder.SetTestNetMagic(testNetMagic)
	builder.AddOutputs(outputs...).AddOutputs(cardano_wallet.TxOutput{
		Addr: multiSigAddr,
	}).AddOutputs(cardano_wallet.TxOutput{
		Addr: multiSigFeeAddr,
	})
	builder.AddInputsWithScript(policyScriptMultiSig, multiSigInputs.Inputs...)
	builder.AddInputsWithScript(policyScriptFeeMultiSig, multiSigFeeInputs.Inputs...)

	fee, err := builder.CalculateFee(0)
	require.NoError(t, err)

	builder.SetFee(fee)

	builder.UpdateOutputAmount(-2, multiSigInputs.Sum-outputsSum)
	builder.UpdateOutputAmount(-1, multiSigFeeInputs.Sum-fee)

	txRaw, txHash, err := builder.Build()
	require.NoError(t, err)

	return hex.EncodeToHex(txRaw), txHash
}
