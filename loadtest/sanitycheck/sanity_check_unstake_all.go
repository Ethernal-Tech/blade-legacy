package sanitycheck

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/umbracle/ethgo"
)

// UnstakeAllTest is a test that unstakes all the stake of a validator
type UnstakeAllTest struct {
	*UnstakeTest
	*RegisterValidatorTest
}

// NewUnstakeAllTest creates a new UnstakeAllTest
func NewUnstakeAllTest(cfg *SanityCheckTestConfig,
	testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) (*UnstakeAllTest, error) {
	unstakeTest, err := NewUnstakeTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	registerValidatorTest, err := NewRegisterValidatorTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	return &UnstakeAllTest{
		UnstakeTest:           unstakeTest,
		RegisterValidatorTest: registerValidatorTest,
	}, nil
}

// Name returns the name of the unstake all test
func (t *UnstakeAllTest) Name() string {
	return "Unstake All Test"
}

// Name returns the name of the unstake all test
// It does the following steps:
// 1. Register a new validator.
// 2. Unstake all the stake of the validator.
// 3. Wait for the epoch ending block.
// 4. Check if the validator is removed from the validator set.
func (t *UnstakeAllTest) Run() error {
	printUxSeparator()

	fmt.Println("Running", t.Name())
	defer fmt.Println("Finished", t.Name())

	validatorAcc, err := t.RegisterValidatorTest.runTest()
	if err != nil {
		return err
	}

	blockNum, err := t.UnstakeTest.unstake(validatorAcc.Ecdsa, ethgo.Ether(1))
	if err != nil {
		return err
	}

	if blockNum%t.config.EpochSize != 0 {
		// if validator unstaked all on the epoch ending block, it will be removed on the next epoch
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

	fmt.Println("Checking if validator is removed from validator set since it unstaked all")

	if extra.Validators == nil || extra.Validators.IsEmpty() {
		return fmt.Errorf("validator set delta is empty on an epoch ending block. Block: %d. EpochSize: %d",
			epochEndingBlock.Number, t.config.EpochSize)
	}

	if len(extra.Validators.Removed) != 1 {
		return fmt.Errorf("expected 1 validator to be removed from the validator set, got %d", len(extra.Validators.Removed))
	}

	fmt.Println("Validator", validatorAcc.Address(), "is removed from the validator set")

	return nil
}
