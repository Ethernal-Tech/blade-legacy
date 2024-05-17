package sanitycheck

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo"
)

// WithdrawRewardsTest represents a withdraw rewards test.
type WithdrawRewardsTest struct {
	*BaseSanityCheckTest
}

// NewWithdrawRewardsTest creates a new WithdrawRewardsTest.
func NewWithdrawRewardsTest(cfg *SanityCheckTestConfig,
	testAccountKey *crypto.ECDSAKey, client *jsonrpc.EthClient) (*WithdrawRewardsTest, error) {
	base, err := NewBaseSanityCheckTest(cfg, testAccountKey, client)
	if err != nil {
		return nil, err
	}

	return &WithdrawRewardsTest{
		BaseSanityCheckTest: base,
	}, nil
}

// Name returns the name of the withdraw rewards test.
func (t *WithdrawRewardsTest) Name() string {
	return "Withdraw Rewards Test"
}

// Run runs the withdraw rewards test.
func (t *WithdrawRewardsTest) Run() error {
	printUxSeparator()

	fmt.Println("Running", t.Name())
	defer fmt.Println("Finished", t.Name())

	validatorKey, err := t.decodePrivateKey(t.config.ValidatorKeys[0])
	if err != nil {
		return err
	}

	if err := t.fundAddress(validatorKey.Address(), ethgo.Ether(1)); err != nil {
		return err
	}

	// lets wait for one epoch so that there are some rewards accumulated
	_, err = t.waitForEpochEnding()
	if err != nil {
		return err
	}

	pendingRewardsBefore, err := t.getPendingRewards(validatorKey.Address())
	if err != nil {
		return fmt.Errorf("failed to get pending rewards: %w", err)
	}

	fmt.Println("Pending rewards for validator before withdrawal", validatorKey.Address(), pendingRewardsBefore)

	if pendingRewardsBefore.Cmp(big.NewInt(0)) == 0 {
		return fmt.Errorf("no pending rewards for validator: %s", validatorKey.Address())
	}

	if err := t.withdrawRewards(validatorKey); err != nil {
		return fmt.Errorf("failed to withdraw rewards: %w", err)
	}

	// check if the pending rewards are zero after withdrawing
	pendingRewardsAfter, err := t.getPendingRewards(validatorKey.Address())
	if err != nil {
		return fmt.Errorf("failed to get pending rewards: %w", err)
	}

	fmt.Println("Pending rewards for validator after withdrawal", validatorKey.Address(), pendingRewardsAfter)

	// check if the pending rewards after withdrawing are less than before
	// we do not check if they are 0 because epoch ending block can arrive before this check,
	// and the validator can get new rewards
	if pendingRewardsAfter.Cmp(pendingRewardsBefore) >= 0 {
		return fmt.Errorf("pending rewards after withdrawing are not less than before: %d < %d",
			pendingRewardsAfter, pendingRewardsBefore)
	}

	return nil
}

// withdrawRewards withdraws the pending rewards for the given validator.
func (t *WithdrawRewardsTest) withdrawRewards(validatorKey *crypto.ECDSAKey) error {
	encoded, err := contractsapi.EpochManager.Abi.Methods["withdrawReward"].Encode([]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to encode withdraw rewards function: %w", err)
	}

	tx := types.NewTx(types.NewLegacyTx(
		types.WithFrom(validatorKey.Address()),
		types.WithTo(&contracts.EpochManagerContract),
		types.WithInput(encoded),
	))

	receipt, err := t.txrelayer.SendTransaction(tx, validatorKey)
	if err != nil {
		return fmt.Errorf("failed to send withdraw rewards transaction: %w", err)
	}

	if receipt.Status != uint64(types.ReceiptSuccess) {
		return fmt.Errorf("withdraw rewards transaction failed on block: %d", receipt.BlockNumber)
	}

	return nil
}

// getPendingRewards returns the pending rewards for the given validator.
func (t *WithdrawRewardsTest) getPendingRewards(validatorAddr types.Address) (*big.Int, error) {
	encoded, err := contractsapi.EpochManager.Abi.Methods["pendingRewards"].Encode([]interface{}{validatorAddr})
	if err != nil {
		return nil, fmt.Errorf("failed to encode pending rewards function: %w", err)
	}

	response, err := t.txrelayer.Call(validatorAddr, contracts.EpochManagerContract, encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to call pending rewards function: %w", err)
	}

	amount, err := common.ParseUint256orHex(&response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pending rewards: %w", err)
	}

	return amount, nil
}
