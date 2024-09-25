package sanitycheck

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
)

// RegisterValidatorTest represents a register validator test.
type RegisterValidatorTest struct {
	*BaseSanityCheckTest
}

// NewRegisterValidatorTest creates a new RegisterValidatorTest.
func NewRegisterValidatorTest(cfg *SanityCheckTestConfig,
	testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) (*RegisterValidatorTest, error) {
	baseSanityCheckTest, err := NewBaseSanityCheckTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	return &RegisterValidatorTest{
		BaseSanityCheckTest: baseSanityCheckTest,
	}, nil
}

// Name returns the name of the register validator test.
func (t *RegisterValidatorTest) Name() string {
	return "Register Validator Test"
}

// Run runs the register validator test.
// It does the following steps:
// 1. Generate a new validator key.
// 2. Fund the new validator address.
// 3. Whitelist the new validator.
// 4. Register the new validator with a stake amount.
// 5. Wait for the epoch ending block.
// 6. Check if the new validator is added to the validator set with its stake.
func (t *RegisterValidatorTest) Run() error {
	printUxSeparator()

	fmt.Println("Running", t.Name())
	defer fmt.Println("Finished", t.Name())

	_, err := t.runTest()

	return err
}

// runTest runs the register validator test.
func (t *RegisterValidatorTest) runTest() (*wallet.Account, error) {
	fundAmount := ethgo.Ether(2)
	stakeAmount := ethgo.Ether(1)

	newValidatorAcc, err := wallet.GenerateAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to generate new validator key: %w", err)
	}

	if err := t.fundAddress(newValidatorAcc.Address(), fundAmount); err != nil {
		return nil, fmt.Errorf("failed to fund new validator address: %w", err)
	}

	bladeAdminKey, err := t.decodePrivateKey(t.config.ValidatorKeys[0])
	if err != nil {
		return nil, err
	}

	if err := t.whitelistValidators(bladeAdminKey, newValidatorAcc.Address()); err != nil {
		return nil, fmt.Errorf("failed to whitelist new validator: %w", err)
	}

	blockNum, err := t.registerValidator(newValidatorAcc, stakeAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to register new validator: %w", err)
	}

	if blockNum%t.config.EpochSize == 0 {
		// if validator was registered on the epoch ending block, it will become active on the next epoch
		blockNum++
	}

	epochEndingBlock, err := t.waitForEpochEnding(&blockNum)
	if err != nil {
		return nil, err
	}

	extra, err := polytypes.GetIbftExtra(epochEndingBlock.ExtraData)
	if err != nil {
		return nil, fmt.Errorf("failed to get ibft extra data for epoch ending block. Error: %w", err)
	}

	fmt.Println("Checking if new validator is added to validator set with its stake")

	if extra.Validators == nil || extra.Validators.IsEmpty() {
		return nil, fmt.Errorf("validator set delta is empty on an epoch ending block. Block: %d. EpochSize: %d",
			epochEndingBlock.Number, t.config.EpochSize)
	}

	if !extra.Validators.Added.ContainsAddress(newValidatorAcc.Address()) {
		return nil, fmt.Errorf("validator %s is not in the added validators. Block: %d. EpochSize: %d",
			newValidatorAcc.Address(), epochEndingBlock.Number, t.config.EpochSize)
	}

	validatorMetaData := extra.Validators.Added.GetValidatorMetadata(newValidatorAcc.Address())
	if validatorMetaData.VotingPower.Cmp(stakeAmount) != 0 {
		return nil, fmt.Errorf("voting power of validator %s is incorrect. Expected: %s, Actual: %s",
			newValidatorAcc.Address(), stakeAmount, validatorMetaData.VotingPower)
	}

	fmt.Println("Validator", newValidatorAcc.Address(), "is added to the new validator set with correct voting power")

	return newValidatorAcc, nil
}

// whitelistValidators adds the given validators to the whitelist on StakeManager contract.
func (t *RegisterValidatorTest) whitelistValidators(bladeAdminKey *crypto.ECDSAKey, validators ...types.Address) error {
	whitelistFn := contractsapi.WhitelistValidatorsStakeManagerFn{
		Validators_: validators,
	}

	encoded, err := whitelistFn.EncodeAbi()
	if err != nil {
		return fmt.Errorf("failed to encode whitelist validators data: %w", err)
	}

	tx := types.NewTx(types.NewLegacyTx(
		types.WithFrom(bladeAdminKey.Address()),
		types.WithTo(&contracts.StakeManagerContract),
		types.WithInput(encoded),
	))

	receipt, err := t.txrelayer.SendTransaction(tx, bladeAdminKey)
	if err != nil {
		return err
	}

	if receipt.Status == uint64(types.ReceiptFailed) {
		return fmt.Errorf("whitelist transaction failed on block %d", receipt.BlockNumber)
	}

	return nil
}

// registerValidator registers the given validator with the given stake amount.
// it does the following steps:
// 1. Approve the stake amount for StakeManager contract.
// 2. Create KOSK signature.
// 3. Create RegisterStakeManagerFn and send the transaction.
func (t *RegisterValidatorTest) registerValidator(validatorAcc *wallet.Account, stakeAmount *big.Int) (uint64, error) {
	// first we need to approve the stake amount
	if err := t.approveNativeERC20(validatorAcc.Ecdsa, stakeAmount, contracts.StakeManagerContract); err != nil {
		return 0, fmt.Errorf("failed to approve stake amount: %w", err)
	}

	// then we create the KOSK signature
	chainID, err := t.client.ChainID()
	if err != nil {
		return 0, fmt.Errorf("failed to get chain ID: %w", err)
	}

	koskSignature, err := signer.MakeKOSKSignature(
		validatorAcc.Bls, validatorAcc.Address(),
		chainID.Int64(), signer.DomainValidatorSet, contracts.StakeManagerContract)
	if err != nil {
		return 0, err
	}

	// then we create register validator txn and send it
	sigMarshal, err := koskSignature.ToBigInt()
	if err != nil {
		return 0, fmt.Errorf("failed to marshal kosk signature: %w", err)
	}

	registerFn := &contractsapi.RegisterStakeManagerFn{
		Signature:   sigMarshal,
		Pubkey:      validatorAcc.Bls.PublicKey().ToBigInt(),
		StakeAmount: stakeAmount,
	}

	encoded, err := registerFn.EncodeAbi()
	if err != nil {
		return 0, fmt.Errorf("failed to encode register validator data: %w", err)
	}

	tx := types.NewTx(types.NewLegacyTx(
		types.WithFrom(validatorAcc.Address()),
		types.WithTo(&contracts.StakeManagerContract),
		types.WithInput(encoded),
	))

	receipt, err := t.txrelayer.SendTransaction(tx, validatorAcc.Ecdsa)
	if err != nil {
		return 0, fmt.Errorf("failed to send register validator transaction: %w", err)
	}

	if receipt.Status != uint64(types.ReceiptSuccess) {
		return 0, fmt.Errorf("register validator transaction failed on block %d", receipt.BlockNumber)
	}

	return receipt.BlockNumber, nil
}
