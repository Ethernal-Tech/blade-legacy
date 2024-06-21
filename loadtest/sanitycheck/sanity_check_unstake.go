package sanitycheck

import (
	"fmt"
	"math/big"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo"
)

// UnstakeTest represents an unstake test.
type UnstakeTest struct {
	*StakeTest
}

// NewUnstakeTest creates a new UnstakeTest.
func NewUnstakeTest(cfg *SanityCheckTestConfig,
	testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) (*UnstakeTest, error) {
	stakeTest, err := NewStakeTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	return &UnstakeTest{
		StakeTest: stakeTest,
	}, nil
}

// Name returns the name of the unstake test.
func (t *UnstakeTest) Name() string {
	return "Unstake Test"
}

// Run runs the unstake test.
// It does the following steps:
// 1. Fund the validator address.
// 2. Unstake the given amount for the validator.
// 3. Wait for the epoch ending block.
// 4. Check if the correct validator stake is in the validator set delta.
func (t *UnstakeTest) Run() error {
	printUxSeparator()

	fmt.Println("Running", t.Name())
	defer fmt.Println("Finished", t.Name())

	validatorKey, err := t.decodePrivateKey(t.config.ValidatorKeys[0])
	if err != nil {
		return err
	}

	amountToUnstake := ethgo.Ether(1)

	if err := t.fundAddress(validatorKey.Address(), amountToUnstake); err != nil {
		return err
	}

	previousStake, err := t.getStake(validatorKey.Address())
	if err != nil {
		return fmt.Errorf("failed to get stake of validator: %s. Error: %w", validatorKey.Address(), err)
	}

	fmt.Println("Stake of validator", validatorKey.Address(), "before unstaking:", previousStake)

	blockNum, err := t.unstake(validatorKey, amountToUnstake)
	if err != nil {
		return fmt.Errorf("failed to stake for validator: %s. Error: %w", validatorKey.Address(), err)
	}

	currentStake, err := t.getStake(validatorKey.Address())
	if err != nil {
		return fmt.Errorf("failed to get new stake of validator: %s. Error: %w", validatorKey.Address(), err)
	}

	fmt.Println("Stake of validator", validatorKey.Address(), "after unstaking:", currentStake)

	expectedStake := previousStake.Sub(previousStake, amountToUnstake)
	if currentStake.Cmp(expectedStake) != 0 {
		return fmt.Errorf("stake amount is incorrect. Expected: %s, Actual: %s", expectedStake, currentStake)
	}

	if blockNum%t.config.EpochSize != 0 {
		// if validator unstaked on the epoch ending block, it will be added on the next epoch
		blockNum++
	}

	epochEndingBlock, err := t.waitForEpochEnding(&blockNum)
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

// unstake unstakes the given amount for the given validator.
func (t *UnstakeTest) unstake(validatorKey *crypto.ECDSAKey, amount *big.Int) (uint64, error) {
	fmt.Println("Unstaking for validator", validatorKey.Address(), "Amount", amount.String())

	s := time.Now().UTC()
	defer func() {
		fmt.Println("Unstaking for validator", validatorKey.Address(), "took", time.Since(s))
	}()

	unstakeFn := &contractsapi.UnstakeStakeManagerFn{
		Amount: amount,
	}

	encoded, err := unstakeFn.EncodeAbi()
	if err != nil {
		return 0, err
	}

	tx := types.NewTx(types.NewLegacyTx(
		types.WithFrom(validatorKey.Address()),
		types.WithTo(&contracts.StakeManagerContract),
		types.WithInput(encoded)))

	receipt, err := t.txrelayer.SendTransaction(tx, validatorKey)
	if err != nil {
		return 0, err
	}

	if receipt.Status == uint64(types.ReceiptFailed) {
		return 0, fmt.Errorf("unstake transaction failed on block %d", receipt.BlockNumber)
	}

	return receipt.BlockNumber, nil
}
