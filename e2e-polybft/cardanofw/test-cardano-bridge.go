package cardanofw

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
)

const (
	ChainIDPrime  = "prime"
	ChainIDVector = "vector"

	BridgeSCAddr = "0xABEF000000000000000000000000000000000000"

	RunAPIOnValidatorID     = 1
	RunRelayerOnValidatorID = 1
)

type TestCardanoBridge struct {
	validatorCount int
	dataDirPath    string

	validators []*TestCardanoValidator

	primeMultisigKeys     []string
	primeMultisigFeeKeys  []string
	vectorMultisigKeys    []string
	vectorMultisigFeeKeys []string

	PrimeMultisigAddr     string
	PrimeMultisigFeeAddr  string
	VectorMultisigAddr    string
	VectorMultisigFeeAddr string

	cluster *framework.TestCluster
}

func NewTestCardanoBridge(dataDirPath string, validatorCount int) *TestCardanoBridge {
	validators := make([]*TestCardanoValidator, validatorCount)

	for i := 0; i < validatorCount; i++ {
		validators[i] = NewTestCardanoValidator(dataDirPath, i+1)
	}

	return &TestCardanoBridge{
		dataDirPath:    dataDirPath,
		validatorCount: validatorCount,
		validators:     validators,
	}
}

func (cb *TestCardanoBridge) CardanoCreateWalletsAndAddresses(
	networkMagicPrime int, networkMagicVector int,
) (err error) {
	err = cb.cardanoCreateWallets()
	if err != nil {
		return err
	}

	err = cb.cardanoPrepareKeys()
	if err != nil {
		return err
	}

	err = cb.cardanoCreateAddresses(networkMagicPrime, networkMagicVector)
	if err != nil {
		return err
	}

	return err
}

func (cb *TestCardanoBridge) StartValidators(t *testing.T, epochSize int) {
	t.Helper()

	cb.cluster = framework.NewTestCluster(t, cb.validatorCount,
		framework.WithEpochSize(epochSize),
	)

	for idx, validator := range cb.validators {
		validator.SetClusterAndServer(cb.cluster, cb.cluster.Servers[idx])
	}
}

func (cb *TestCardanoBridge) WaitForValidatorsReady(t *testing.T) {
	t.Helper()

	cb.cluster.WaitForReady(t)
}

func (cb *TestCardanoBridge) StopValidators() {
	cb.cluster.Stop()
}

func (cb *TestCardanoBridge) RegisterChains(
	primeTokenSupply *big.Int,
	primeBlockfrostURL string,
	vectorTokenSupply *big.Int,
	vectorBlockfrostURL string,
) (err error) {
	for _, validator := range cb.validators {
		err = validator.RegisterChain(
			ChainIDPrime, cb.PrimeMultisigAddr, cb.PrimeMultisigFeeAddr,
			primeTokenSupply, primeBlockfrostURL,
		)
		if err != nil {
			return err
		}

		err = validator.RegisterChain(
			ChainIDVector, cb.VectorMultisigAddr, cb.VectorMultisigFeeAddr,
			vectorTokenSupply, vectorBlockfrostURL,
		)
		if err != nil {
			return err
		}
	}

	return err
}

func (cb *TestCardanoBridge) GenerateConfigs(
	primeNetworkAddress string,
	primeNetworkMagic int,
	primeBlockfrostURL string,
	vectorNetworkAddress string,
	vectorNetworkMagic int,
	vectorBlockfrostURL string,
	apiPortStart int,
	apiKey string,
) (err error) {
	for idx, validator := range cb.validators {
		err = validator.GenerateConfigs(
			primeNetworkAddress,
			primeNetworkMagic,
			primeBlockfrostURL,
			vectorNetworkAddress,
			vectorNetworkMagic,
			vectorBlockfrostURL,
			apiPortStart+idx,
			apiKey,
		)
		if err != nil {
			return err
		}
	}

	return err
}

func (cb *TestCardanoBridge) StartValidatorComponents(ctx context.Context) (err error) {
	for _, validator := range cb.validators {
		validator.StartValidatorComponents(ctx, RunAPIOnValidatorID == validator.ID)
	}

	return err
}

func (cb *TestCardanoBridge) StartRelayer(ctx context.Context) (err error) {
	for _, validator := range cb.validators {
		if RunRelayerOnValidatorID == validator.ID {
			go func(config string) {
				_ = RunCommandContext(ctx, ResolveApexBridgeBinary(), []string{
					"run-relayer",
					"--config", config,
				}, os.Stdout)
			}(validator.GetRelayerConfig())
		}
	}

	return err
}

func (cb *TestCardanoBridge) GetBridgingAPI() (string, error) {
	for _, validator := range cb.validators {
		if validator.ID == RunAPIOnValidatorID {
			if validator.APIPort == 0 {
				return "", fmt.Errorf("api port not defined")
			}

			return fmt.Sprintf("localhost:%d", validator.APIPort), nil
		}
	}

	return "", fmt.Errorf("not running API")
}

func (cb *TestCardanoBridge) cardanoCreateWallets() (err error) {
	for _, validator := range cb.validators {
		err = validator.CardanoWalletCreate(ChainIDPrime)
		if err != nil {
			return err
		}

		err = validator.CardanoWalletCreate(ChainIDVector)
		if err != nil {
			return err
		}
	}

	return err
}

func (cb *TestCardanoBridge) cardanoPrepareKeys() (err error) {
	cb.primeMultisigKeys = make([]string, cb.validatorCount)
	cb.primeMultisigFeeKeys = make([]string, cb.validatorCount)
	cb.vectorMultisigKeys = make([]string, cb.validatorCount)
	cb.vectorMultisigFeeKeys = make([]string, cb.validatorCount)

	for idx, validator := range cb.validators {
		primeWallet, err := validator.GetCardanoWallet(ChainIDPrime)
		if err != nil {
			return err
		}

		cb.primeMultisigKeys[idx] = primeWallet.Multisig.VerifyingKey.Hex
		cb.primeMultisigFeeKeys[idx] = primeWallet.MultisigFee.VerifyingKey.Hex

		vectorWallet, err := validator.GetCardanoWallet(ChainIDVector)
		if err != nil {
			return err
		}

		cb.vectorMultisigKeys[idx] = vectorWallet.Multisig.VerifyingKey.Hex
		cb.vectorMultisigFeeKeys[idx] = vectorWallet.MultisigFee.VerifyingKey.Hex
	}

	return err
}

func (cb *TestCardanoBridge) cardanoCreateAddresses(
	networkMagicPrime int, networkMagicVector int,
) (err error) {
	cb.PrimeMultisigAddr, err = cb.cardanoCreateAddress(networkMagicPrime, cb.primeMultisigKeys)
	if err != nil {
		return err
	}

	cb.PrimeMultisigFeeAddr, err = cb.cardanoCreateAddress(networkMagicPrime, cb.primeMultisigFeeKeys)
	if err != nil {
		return err
	}

	cb.VectorMultisigAddr, err = cb.cardanoCreateAddress(networkMagicVector, cb.vectorMultisigKeys)
	if err != nil {
		return err
	}

	cb.VectorMultisigFeeAddr, err = cb.cardanoCreateAddress(networkMagicVector, cb.vectorMultisigFeeKeys)
	if err != nil {
		return err
	}

	return err
}

func (cb *TestCardanoBridge) cardanoCreateAddress(networkMagic int, keys []string) (string, error) {
	args := []string{
		"create-address",
		"--testnet", fmt.Sprint(networkMagic),
	}

	for _, key := range keys {
		args = append(args, "--key", key)
	}

	var outb bytes.Buffer

	err := RunCommand(ResolveApexBridgeBinary(), args, io.MultiWriter(os.Stdout, &outb))
	if err != nil {
		return "", err
	}

	result := outb.String()
	result = strings.TrimSpace(strings.ReplaceAll(result, "Address = ", ""))

	return result, nil
}
