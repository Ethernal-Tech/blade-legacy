package cardanofw

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"sync"
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

type CardanoBridgeOption func(*TestCardanoBridge)

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

	apiPortStart int
	apiKey       string

	vectorTTLInc uint64
	primeTTLInc  uint64
}

func WithAPIPortStart(apiPortStart int) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.apiPortStart = apiPortStart
	}
}

func WithAPIKey(apiKey string) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.apiKey = apiKey
	}
}

func WithVectorTTLInc(ttlInc uint64) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.vectorTTLInc = ttlInc
	}
}

func WithPrimeTTLInc(ttlInc uint64) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.primeTTLInc = ttlInc
	}
}

func NewTestCardanoBridge(
	dataDirPath string, validatorCount int, opts ...CardanoBridgeOption,
) *TestCardanoBridge {
	validators := make([]*TestCardanoValidator, validatorCount)

	for i := 0; i < validatorCount; i++ {
		validators[i] = NewTestCardanoValidator(dataDirPath, i+1)
	}

	bridge := &TestCardanoBridge{
		dataDirPath:    dataDirPath,
		validatorCount: validatorCount,
		validators:     validators,
	}

	for _, opt := range opts {
		opt(bridge)
	}

	return bridge
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
	if cb.cluster != nil {
		cb.cluster.Stop()
	}
}

func (cb *TestCardanoBridge) RegisterChains(
	primeTokenSupply *big.Int,
	primeOgmiosURL string,
	vectorTokenSupply *big.Int,
	vectorOgmiosURL string,
) error {
	errs := make([]error, len(cb.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(cb.validators))

	for i, validator := range cb.validators {
		go func(validator *TestCardanoValidator, indx int) {
			defer wg.Done()

			errs[indx] = validator.RegisterChain(
				ChainIDPrime, cb.PrimeMultisigAddr, cb.PrimeMultisigFeeAddr,
				primeTokenSupply,
			)
			if errs[indx] != nil {
				return
			}

			errs[indx] = validator.RegisterChain(
				ChainIDVector, cb.VectorMultisigAddr, cb.VectorMultisigFeeAddr,
				vectorTokenSupply,
			)
			if errs[indx] != nil {
				return
			}
		}(validator, i)
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (cb *TestCardanoBridge) GenerateConfigs(
	primeNetworkAddress string,
	primeNetworkMagic int,
	primeOgmiosURL string,
	vectorNetworkAddress string,
	vectorNetworkMagic int,
	vectorOgmiosURL string,
) error {
	errs := make([]error, len(cb.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(cb.validators))

	for i, validator := range cb.validators {
		go func(validator *TestCardanoValidator, indx int) {
			defer wg.Done()

			telemetryConfig := ""
			if indx == 0 {
				telemetryConfig = "0.0.0.0:5001,localhost:8126"
			}

			errs[indx] = validator.GenerateConfigs(
				primeNetworkAddress,
				primeNetworkMagic,
				primeOgmiosURL,
				cb.primeTTLInc,
				vectorNetworkAddress,
				vectorNetworkMagic,
				vectorOgmiosURL,
				cb.vectorTTLInc,
				cb.apiPortStart+indx,
				cb.apiKey,
				telemetryConfig,
			)
		}(validator, i)
	}

	wg.Wait()

	return errors.Join(errs...)
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

			return fmt.Sprintf("http://localhost:%d", validator.APIPort), nil
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

		cb.primeMultisigKeys[idx] = hex.EncodeToString(primeWallet.Multisig.GetVerificationKey())
		cb.primeMultisigFeeKeys[idx] = hex.EncodeToString(primeWallet.MultisigFee.GetVerificationKey())

		vectorWallet, err := validator.GetCardanoWallet(ChainIDVector)
		if err != nil {
			return err
		}

		cb.vectorMultisigKeys[idx] = hex.EncodeToString(vectorWallet.Multisig.GetVerificationKey())
		cb.vectorMultisigFeeKeys[idx] = hex.EncodeToString(vectorWallet.MultisigFee.GetVerificationKey())
	}

	return err
}

func (cb *TestCardanoBridge) cardanoCreateAddresses(
	networkMagicPrime int, networkMagicVector int,
) error {
	errs := make([]error, 4)
	wg := sync.WaitGroup{}

	wg.Add(4)

	go func() {
		defer wg.Done()

		cb.PrimeMultisigAddr, errs[0] = cb.cardanoCreateAddress(networkMagicPrime, cb.primeMultisigKeys)
	}()

	go func() {
		defer wg.Done()

		cb.PrimeMultisigFeeAddr, errs[1] = cb.cardanoCreateAddress(networkMagicPrime, cb.primeMultisigFeeKeys)
	}()

	go func() {
		defer wg.Done()

		cb.VectorMultisigAddr, errs[2] = cb.cardanoCreateAddress(networkMagicVector, cb.vectorMultisigKeys)
	}()

	go func() {
		defer wg.Done()

		cb.VectorMultisigFeeAddr, errs[3] = cb.cardanoCreateAddress(networkMagicVector, cb.vectorMultisigFeeKeys)
	}()

	wg.Wait()

	return errors.Join(errs...)
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

	result := strings.TrimSpace(strings.ReplaceAll(outb.String(), "Address = ", ""))

	return result, nil
}
