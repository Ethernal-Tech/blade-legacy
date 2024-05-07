package cardanofw

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"path"
	"strings"

	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/fxamacker/cbor/v2"
)

func PopulateAddress(ctx context.Context,
	clusterID int,
	txProvider wallet.ITxProvider,
	dirPath string,
	amount uint64,
	receiver string,
	testnetMagic uint,
	keyID uint,
	nodesCnt uint) error {
	if keyID < 1 && keyID > nodesCnt {
		return fmt.Errorf("invalid key id")
	}

	clusterPath := path.Join(dirPath, strings.Join([]string{"cluster", fmt.Sprint(clusterID)}, "-"))

	keyFileName := strings.Join([]string{"utxo", fmt.Sprint(keyID)}, "")

	sKey, err := wallet.NewKey(path.Join(clusterPath, "utxo-keys", strings.Join([]string{keyFileName, "skey"}, ".")))
	if err != nil {
		return err
	}

	sKeyBytes, err := sKey.GetKeyBytes()
	if err != nil {
		return err
	}

	vKey, err := wallet.NewKey(path.Join(clusterPath, "utxo-keys", strings.Join([]string{keyFileName, "vkey"}, ".")))
	if err != nil {
		return err
	}

	vKeyBytes, err := vKey.GetKeyBytes()
	if err != nil {
		return err
	}

	genesisWallet := wallet.NewWallet(vKeyBytes, sKeyBytes, "")

	genesisAddress, _, err := wallet.GetWalletAddress(genesisWallet, testnetMagic)
	if err != nil {
		return err
	}

	protocolParams, err := txProvider.GetProtocolParameters(ctx)
	if err != nil {
		return err
	}

	qtd, err := txProvider.GetTip(ctx)
	if err != nil {
		return err
	}

	outputs := []wallet.TxOutput{
		{
			Addr:   receiver,
			Amount: amount,
		},
	}

	utxos, err := txProvider.GetUtxos(ctx, genesisAddress)
	if err != nil {
		return err
	}

	if len(utxos) == 0 {
		return fmt.Errorf("no utxos at given address")
	}

	inputs := []wallet.TxInput{
		{
			Hash:  utxos[0].Hash,
			Index: utxos[0].Index,
		},
	}

	rawTx, txHash, err := CreateTx(testnetMagic, protocolParams, qtd.Slot+TTLSlotNumberInc, []byte{}, outputs, inputs)
	if err != nil {
		return err
	}

	signedTx, err := wallet.SignTx(rawTx, txHash, genesisWallet)
	if err != nil {
		return err
	}

	return txProvider.SubmitTx(ctx, signedTx)
}

const TTLSlotNumberInc = 200

// CreateTx creates tx and returns cbor of raw transaction data, tx hash and error
func CreateTx(testNetMagic uint,
	protocolParams []byte,
	timeToLive uint64,
	metadataBytes []byte,
	outputs []wallet.TxOutput,
	inputs []wallet.TxInput) ([]byte, string, error) {
	builder, err := wallet.NewTxBuilder()
	if err != nil {
		return nil, "", err
	}

	defer builder.Dispose()

	builder.SetProtocolParameters(protocolParams).SetTimeToLive(timeToLive)
	builder.SetMetaData(metadataBytes).SetTestNetMagic(testNetMagic)
	builder.AddOutputs(outputs...)
	builder.AddInputs(inputs...)

	fee, err := builder.CalculateFee(0)
	if err != nil {
		return nil, "", err
	}

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

type SigningKey struct {
	private []byte
	public  []byte
}

func NewSigningKey(s string) SigningKey {
	private := decodeCbor(s)

	return SigningKey{
		private: private,
		public:  wallet.GetVerificationKeyFromSigningKey(private),
	}
}

func (sk SigningKey) GetSigningKey() []byte {
	return sk.private
}

func (sk SigningKey) GetVerificationKey() []byte {
	return sk.public
}

func decodeCbor(s string) (r []byte) {
	b, _ := hex.DecodeString(s)
	_ = cbor.Unmarshal(b, &r)

	return r
}

func CreateMetaData(v *big.Int) ([]byte, error) {
	metadata := map[string]interface{}{
		"0": map[string]interface{}{
			"type":  "multi",
			"value": v.String(),
		},
	}

	return json.Marshal(metadata)
}

type TxInputInfos struct {
	TestNetMagic uint
	MultiSig     *TxInputInfo
	MultiSigFee  *TxInputInfo
}

func NewTxInputInfos(
	keyHashesMultiSig []string, keyHashesMultiSigFee []string, testNetMagic uint,
) (
	*TxInputInfos, error,
) {
	result := [2]*TxInputInfo{}

	for i, keyHashes := range [][]string{keyHashesMultiSig, keyHashesMultiSigFee} {
		ps, err := wallet.NewPolicyScript(keyHashes, len(keyHashes)*2/3+1)
		if err != nil {
			return nil, err
		}

		addr, err := ps.CreateMultiSigAddress(testNetMagic)
		if err != nil {
			return nil, err
		}

		result[i] = &TxInputInfo{
			PolicyScript: ps,
			Address:      addr,
		}
	}

	return &TxInputInfos{
		TestNetMagic: testNetMagic,
		MultiSig:     result[0],
		MultiSigFee:  result[1],
	}, nil
}

func (txinfos *TxInputInfos) Calculate(utxos, utxosFee []wallet.Utxo, desired, desiredFee uint64) error {
	if err := txinfos.MultiSig.Calculate(utxos, desired); err != nil {
		return err
	}

	return txinfos.MultiSigFee.Calculate(utxosFee, desiredFee)
}

func (txinfos *TxInputInfos) CalculateWithRetriever(
	ctx context.Context, retriever wallet.IUTxORetriever, desired, desiredFee uint64,
) error {
	if err := txinfos.MultiSig.CalculateWithRetriever(ctx, retriever, desired); err != nil {
		return err
	}

	return txinfos.MultiSigFee.CalculateWithRetriever(ctx, retriever, desiredFee)
}

type TxInputUTXOs struct {
	Inputs    []wallet.TxInput
	InputsSum uint64
}

type TxInputInfo struct {
	TxInputUTXOs
	PolicyScript *wallet.PolicyScript
	Address      string
}

func (txinfo *TxInputInfo) Calculate(utxos []wallet.Utxo, desired uint64) error {
	// Loop through utxos to find first input with enough tokens
	// If we don't have this UTXO we need to use more of them
	var amountSum = uint64(0)

	chosenUTXOs := make([]wallet.TxInput, 0, len(utxos))

	for _, utxo := range utxos {
		if utxo.Amount >= desired {
			txinfo.Inputs = []wallet.TxInput{
				{
					Hash:  utxo.Hash,
					Index: utxo.Index,
				},
			}
			txinfo.InputsSum = utxo.Amount

			return nil
		}

		amountSum += utxo.Amount
		chosenUTXOs = append(chosenUTXOs, wallet.TxInput{
			Hash:  utxo.Hash,
			Index: utxo.Index,
		})

		if amountSum >= desired {
			txinfo.Inputs = chosenUTXOs
			txinfo.InputsSum = amountSum

			return nil
		}
	}

	return fmt.Errorf("not enough funds to generate the transaction: %d available vs %d required", amountSum, desired)
}

func (txinfo *TxInputInfo) CalculateWithRetriever(
	ctx context.Context, retriever wallet.IUTxORetriever, desired uint64,
) error {
	utxos, err := retriever.GetUtxos(ctx, txinfo.Address)
	if err != nil {
		return err
	}

	return txinfo.Calculate(utxos, desired)
}