package cardanofw

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

func SendTx(ctx context.Context,
	txProvider wallet.ITxProvider,
	cardanoWallet wallet.IWallet,
	amount uint64,
	receiver string,
	testnetMagic int,
	metadata []byte,
) (string, error) {
	cardanoWalletAddr, _, err := wallet.GetWalletAddress(cardanoWallet, uint(testnetMagic))
	if err != nil {
		return "", err
	}

	protocolParams, err := txProvider.GetProtocolParameters(ctx)
	if err != nil {
		return "", err
	}

	qtd, err := txProvider.GetTip(ctx)
	if err != nil {
		return "", err
	}

	outputs := []wallet.TxOutput{
		{
			Addr:   receiver,
			Amount: amount,
		},
	}

	utxos, err := txProvider.GetUtxos(ctx, cardanoWalletAddr)
	if err != nil {
		return "", err
	}

	if len(utxos) == 0 {
		return "", fmt.Errorf("no utxos at given address")
	}

	inputs := []wallet.TxInput{
		{
			Hash:  utxos[0].Hash,
			Index: utxos[0].Index,
		},
	}
	sendingAmount := utxos[0].Amount

	rawTx, txHash, err := CreateTx(uint(testnetMagic), protocolParams, qtd.Slot+TTLSlotNumberInc, metadata,
		outputs, inputs, cardanoWalletAddr, sendingAmount, amount)
	if err != nil {
		return "", err
	}

	signedTx, err := wallet.SignTx(rawTx, txHash, cardanoWallet)
	if err != nil {
		return "", err
	}

	return txHash, txProvider.SubmitTx(ctx, signedTx)
}

func GetGenesisWalletFromCluster(
	dirPath string,
	keyID uint,
) (*wallet.Wallet, error) {
	keyFileName := strings.Join([]string{"utxo", fmt.Sprint(keyID)}, "")

	sKey, err := wallet.NewKey(path.Join(dirPath, "utxo-keys", strings.Join([]string{keyFileName, "skey"}, ".")))
	if err != nil {
		return nil, err
	}

	sKeyBytes, err := sKey.GetKeyBytes()
	if err != nil {
		return nil, err
	}

	vKey, err := wallet.NewKey(path.Join(dirPath, "utxo-keys", strings.Join([]string{keyFileName, "vkey"}, ".")))
	if err != nil {
		return nil, err
	}

	vKeyBytes, err := vKey.GetKeyBytes()
	if err != nil {
		return nil, err
	}

	return wallet.NewWallet(vKeyBytes, sKeyBytes, ""), nil
}

const TTLSlotNumberInc = 200

// CreateTx creates tx and returns cbor of raw transaction data, tx hash and error
func CreateTx(testNetMagic uint,
	protocolParams []byte,
	timeToLive uint64,
	metadataBytes []byte,
	outputs []wallet.TxOutput,
	inputs []wallet.TxInput,
	changeAddress string,
	totalInput uint64,
	totalOutput uint64) ([]byte, string, error) {
	builder, err := wallet.NewTxBuilder()
	if err != nil {
		return nil, "", err
	}

	defer builder.Dispose()

	builder.SetProtocolParameters(protocolParams).SetTimeToLive(timeToLive)

	if len(metadataBytes) != 0 {
		builder.SetMetaData(metadataBytes)
	}

	builder.SetTestNetMagic(testNetMagic)

	// Add change
	outputs = append(outputs, wallet.TxOutput{
		Addr:   changeAddress,
		Amount: 0,
	})

	builder.AddOutputs(outputs...)
	builder.AddInputs(inputs...)

	fee, err := builder.CalculateFee(0)
	if err != nil {
		return nil, "", err
	}

	change := totalInput - totalOutput - fee
	if change < wallet.MinUTxODefaultValue {
		return []byte{}, "", fmt.Errorf("change too small, should be greater or equal than %v but change = %v",
			wallet.MinUTxODefaultValue, change)
	}

	builder.UpdateOutputAmount(len(outputs)-1, change)

	builder.SetFee(fee)

	return builder.Build()
}

// CreateTxWitness creates cbor of vkey+signature pair of tx hash
func CreateTxWitness(txHash string, key wallet.ISigner) ([]byte, error) {
	return wallet.CreateTxWitness(txHash, key)
}

// AssembleTxWitnesses assembles all witnesses in final cbor of signed tx
func AssembleTxWitnesses(txRaw []byte, witnesses [][]byte) ([]byte, error) {
	return wallet.AssembleTxWitnesses(txRaw, witnesses)
}
