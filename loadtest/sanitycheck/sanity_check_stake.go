package sanitycheck

import (
	"fmt"
	"math/big"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo"
)

// StakeTest represents a stake test.
type StakeTest struct {
	*BaseSanityCheckTest
}

// NewStakeTest creates a new StakeTest.
func NewStakeTest(cfg *SanityCheckTestConfig,
	testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) (*StakeTest, error) {
	base, err := NewBaseSanityCheckTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	return &StakeTest{
		BaseSanityCheckTest: base,
	}, nil
}

// Name returns the name of the stake test.
func (t *StakeTest) Name() string {
	return "Stake Test"
}

// Run runs the stake test.
// It does the following steps:
// 1. Fund the validator address.
// 2. Stake the given amount for the validator.
// 3. Wait for the epoch ending block.
// 4. Check if the correct validator stake is in the validator set delta.
func (t *StakeTest) Run() error {
	printUxSeparator()

	fmt.Println("Running", t.Name())
	defer fmt.Println("Finished", t.Name())

	validatorKey, err := t.decodePrivateKey(t.config.ValidatorKeys[0])
	if err != nil {
		return err
	}

	amountToStake := ethgo.Ether(1)

	if err := t.fundAddress(validatorKey.Address(), ethgo.Ether(1)); err != nil {
		return err
	}

	previousStake, err := t.getStake(validatorKey.Address())
	if err != nil {
		return fmt.Errorf("failed to get stake of validator: %s. Error: %w", validatorKey.Address(), err)
	}

	fmt.Println("Stake of validator", validatorKey.Address(), "before staking:", previousStake)

	if err := t.stake(validatorKey, amountToStake); err != nil {
		return fmt.Errorf("failed to stake for validator: %s. Error: %w", validatorKey.Address(), err)
	}

	currentStake, err := t.getStake(validatorKey.Address())
	if err != nil {
		return fmt.Errorf("failed to get new stake of validator: %s. Error: %w", validatorKey.Address(), err)
	}

	fmt.Println("Stake of validator", validatorKey.Address(), "after staking:", currentStake)

	expectedStake := previousStake.Add(previousStake, amountToStake)
	if currentStake.Cmp(expectedStake) != 0 {
		return fmt.Errorf("stake amount is incorrect. Expected: %s, Actual: %s", expectedStake, currentStake)
	}

	epochEndingBlock, err := t.waitForEpochEnding()
	if err != nil {
		return err
	}

	extra, err := polybft.GetIbftExtra(epochEndingBlock.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to get ibft extra data for epoch ending block. Error: %w", err)
	}

	fmt.Println("Checking if correct validator stake is in validator set delta")

	if extra.Validators == nil || extra.Validators.IsEmpty() {
		return fmt.Errorf("validator set delta is empty on an epoch ending block")
	}

	if !extra.Validators.Updated.ContainsAddress(validatorKey.Address()) {
		return fmt.Errorf("validator %s is not in the updated validator set", validatorKey.Address())
	}

	validatorMetaData := extra.Validators.Updated.GetValidatorMetadata(validatorKey.Address())
	if validatorMetaData.VotingPower.Cmp(currentStake) != 0 {
		return fmt.Errorf("voting power of validator %s is incorrect. Expected: %s, Actual: %s",
			validatorKey.Address(), currentStake, validatorMetaData.VotingPower)
	}

	fmt.Println("Validator", validatorKey.Address(), "is in the updated validator set with correct voting power")

	return nil
}

// stake stakes the given amount for the given validator.
func (t *StakeTest) stake(validatorKey *crypto.ECDSAKey, amount *big.Int) error {
	if err := t.approveNativeERC20(validatorKey, amount, contracts.StakeManagerContract); err != nil {
		return err
	}

	fmt.Println("Staking for validator", validatorKey.Address(), "Amount", amount.String())

	s := time.Now().UTC()
	defer func() {
		fmt.Println("Staking for validator", validatorKey.Address(), "took", time.Since(s))
	}()

	stakeFn := &contractsapi.StakeStakeManagerFn{
		Amount: amount,
	}

	encoded, err := stakeFn.EncodeAbi()
	if err != nil {
		return err
	}

	tx := types.NewTx(types.NewLegacyTx(types.WithFrom(
		validatorKey.Address()),
		types.WithTo(&contracts.StakeManagerContract),
		types.WithInput(encoded)))

	receipt, err := t.txrelayer.SendTransaction(tx, validatorKey)
	if err != nil {
		return err
	}

	if receipt.Status == uint64(types.ReceiptFailed) {
		return fmt.Errorf("stake transaction failed on block %d", receipt.BlockNumber)
	}

	return nil
}

// getStake returns the stake of the given validator on the StakeManager contract.
func (t *StakeTest) getStake(address types.Address) (*big.Int, error) {
	stakeOfFn := &contractsapi.StakeOfStakeManagerFn{
		Validator: address,
	}

	encode, err := stakeOfFn.EncodeAbi()
	if err != nil {
		return nil, err
	}

	response, err := t.txrelayer.Call(t.testAccountKey.Address(), contracts.StakeManagerContract, encode)
	if err != nil {
		return nil, err
	}

	return common.ParseUint256orHex(&response)
}
