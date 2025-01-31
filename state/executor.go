package state

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/hashicorp/go-hclog"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/state/runtime/addresslist"
	"github.com/0xPolygon/polygon-edge/state/runtime/evm"
	"github.com/0xPolygon/polygon-edge/state/runtime/precompiled"
	"github.com/0xPolygon/polygon-edge/state/runtime/tracer"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	SpuriousDragonMaxCodeSize = 24576
	TxPoolMaxInitCodeSize     = 2 * SpuriousDragonMaxCodeSize

	TxGas                 uint64 = 21000 // Per transaction not creating a contract
	TxGasContractCreation uint64 = 53000 // Per transaction that creates a contract

	TxAccessListAddressGas    uint64 = 2400 // Per address specified in EIP 2930 access list
	TxAccessListStorageKeyGas uint64 = 1900 // Per storage key specified in EIP 2930 access list
)

// GetHashByNumber returns the hash function of a block number
type GetHashByNumber = func(i uint64) types.Hash

type GetHashByNumberHelper = func(*types.Header) GetHashByNumber

// Executor is the main entity
type Executor struct {
	logger  hclog.Logger
	config  *chain.Params
	state   State
	GetHash GetHashByNumberHelper

	PostHook         func(txn *Transition)
	GenesisPostHook  func(*Transition) error
	GetPendingTxHook func(types.Hash) (*types.Transaction, bool)

	IsL1OriginatedToken bool
}

// NewExecutor creates a new executor
func NewExecutor(config *chain.Params, s State, logger hclog.Logger) *Executor {
	return &Executor{
		logger: logger,
		config: config,
		state:  s,
	}
}

func (e *Executor) WriteGenesis(
	alloc map[types.Address]*chain.GenesisAccount,
	initialStateRoot types.Hash) (types.Hash, error) {
	snap, err := e.state.NewSnapshot(initialStateRoot)
	if err != nil {
		return types.Hash{}, err
	}

	txn := NewTxn(snap)
	config := e.config.Forks.At(0)

	env := runtime.TxContext{
		ChainID: e.config.ChainID,
	}

	transition := NewTransition(e.logger, config, snap, txn)
	transition.ctx = env

	for addr, account := range alloc {
		if account.Balance != nil {
			txn.AddBalance(addr, account.Balance)
		}

		if account.Nonce != 0 {
			txn.SetNonce(addr, account.Nonce)
		}

		if len(account.Code) != 0 {
			txn.SetCode(addr, account.Code)
		}

		for key, value := range account.Storage {
			txn.SetState(addr, key, value)
		}
	}

	if e.GenesisPostHook != nil {
		if err := e.GenesisPostHook(transition); err != nil {
			return types.Hash{}, fmt.Errorf("Error writing genesis block: %w", err)
		}
	}

	objs, err := txn.Commit(false)
	if err != nil {
		return types.Hash{}, err
	}

	_, root, err := snap.Commit(objs)
	if err != nil {
		return types.Hash{}, err
	}

	return types.BytesToHash(root), nil
}

// GetDumpTree function returns accounts based on the selected criteria.
func (e *Executor) GetDumpTree(dump *Dump, parentHash types.Hash,
	block *types.Block, opts *DumpInfo) ([]byte, error) {
	txn, err := e.ProcessBlock(parentHash, block, types.BytesToAddress(block.Header.Miner))
	if err != nil {
		return nil, err
	}

	next, err := txn.state.GetDumpTree(dump, opts, false)
	if err != nil {
		return nil, err
	}

	snap, err := e.state.NewSnapshot(block.Header.StateRoot)
	if err != nil {
		return nil, err
	}

	dump.Root = snap.GetRootHash().Bytes()

	return next, nil
}

// Verbosity sets the log verbosity ceiling.
func (e *Executor) Verbosity(level int) (string, error) {
	if level < int(hclog.NoLevel) || level > int(hclog.Off) {
		return hclog.Level(level).String(), fmt.Errorf("invalid log level: %d", level)
	}

	e.logger.SetLevel(hclog.Level(level))

	return hclog.Level(level).String(), nil
}

// ProcessBlock already does all the handling of the whole process
func (e *Executor) ProcessBlock(
	parentRoot types.Hash,
	block *types.Block,
	blockCreator types.Address,
) (*Transition, error) {
	e.logger.Debug("[Executor.ProcessBlock] started...",
		"block number", block.Number(),
		"block hash", block.Hash(),
		"parent state root", parentRoot,
		"block state root", block.Header.StateRoot,
		"txs count", len(block.Transactions))

	txn, err := e.BeginTxn(parentRoot, block.Header, blockCreator)
	if err != nil {
		return nil, err
	}

	var (
		buf    bytes.Buffer
		logLvl = e.logger.GetLevel()
	)

	for _, t := range block.Transactions {
		if t.Gas() > block.Header.GasLimit {
			return nil, runtime.ErrOutOfGas
		}

		if t.From() == emptyFrom && t.Type() != types.StateTxType {
			if poolTx, ok := e.GetPendingTxHook(t.Hash()); ok {
				t.SetFrom(poolTx.From())
			}
		}

		if err = txn.Write(t); err != nil {
			e.logger.Error("failed to write transaction to the block", "tx", t, "err", err)

			return nil, err
		}

		if logLvl < hclog.Debug {
			buf.WriteString(t.String())
			buf.WriteString("\n")
		}
	}

	if logLvl <= hclog.Debug {
		var (
			logMsg  = "[Executor.ProcessBlock] finished."
			logArgs = []interface{}{"txs count", len(block.Transactions)}
		)

		if buf.Len() > 0 {
			logArgs = append(logArgs, "txs", buf.String())
		}

		e.logger.Log(logLvl, logMsg, logArgs...)
	}

	return txn, nil
}

// GetForksInTime returns the active forks at the given block height
func (e *Executor) GetForksInTime(blockNumber uint64) chain.ForksInTime {
	return e.config.Forks.At(blockNumber)
}

func (e *Executor) BeginTxn(
	parentRoot types.Hash,
	header *types.Header,
	coinbaseReceiver types.Address,
) (*Transition, error) {
	forkConfig := e.config.Forks.At(header.Number)

	snap, err := e.state.NewSnapshot(parentRoot)
	if err != nil {
		return nil, err
	}

	burnContract := types.ZeroAddress
	if forkConfig.London {
		burnContract, err = e.config.CalculateBurnContract(header.Number)
		if err != nil {
			return nil, err
		}
	}

	newTxn := NewTxn(snap)

	txCtx := runtime.TxContext{
		Coinbase:     coinbaseReceiver,
		Timestamp:    header.Timestamp,
		Number:       header.Number,
		Difficulty:   types.BytesToHash(new(big.Int).SetUint64(header.Difficulty).Bytes()),
		BaseFee:      new(big.Int).SetUint64(header.BaseFee),
		GasLimit:     header.GasLimit,
		ChainID:      e.config.ChainID,
		BurnContract: burnContract,
	}

	t := NewTransition(e.logger, forkConfig, snap, newTxn)
	t.PostHook = e.PostHook
	t.getHash = e.GetHash(header)
	t.ctx = txCtx
	t.gasPool = txCtx.GasLimit

	t.isL1OriginatedToken = e.IsL1OriginatedToken

	// enable contract deployment allow list (if any)
	if e.config.ContractDeployerAllowList != nil {
		t.deploymentAllowList = addresslist.NewAddressList(t, contracts.AllowListContractsAddr)
	}

	if e.config.ContractDeployerBlockList != nil {
		t.deploymentBlockList = addresslist.NewAddressList(t, contracts.BlockListContractsAddr)
	}

	// enable transactions allow list (if any)
	if e.config.TransactionsAllowList != nil {
		t.txnAllowList = addresslist.NewAddressList(t, contracts.AllowListTransactionsAddr)
	}

	if e.config.TransactionsBlockList != nil {
		t.txnBlockList = addresslist.NewAddressList(t, contracts.BlockListTransactionsAddr)
	}

	// enable transactions allow list (if any)
	if e.config.BridgeAllowList != nil {
		t.bridgeAllowList = addresslist.NewAddressList(t, contracts.AllowListBridgeAddr)
	}

	if e.config.BridgeBlockList != nil {
		t.bridgeBlockList = addresslist.NewAddressList(t, contracts.BlockListBridgeAddr)
	}

	return t, nil
}

type Transition struct {
	logger hclog.Logger

	// dummy
	snap Snapshot

	config  chain.ForksInTime
	state   *Txn
	getHash GetHashByNumber
	ctx     runtime.TxContext
	gasPool uint64

	// result
	receipts []*types.Receipt
	totalGas uint64

	PostHook func(t *Transition)

	// runtimes
	evm         *evm.EVM
	precompiles *precompiled.Precompiled

	// allow list runtimes
	deploymentAllowList *addresslist.AddressList
	deploymentBlockList *addresslist.AddressList
	txnAllowList        *addresslist.AddressList
	txnBlockList        *addresslist.AddressList
	bridgeAllowList     *addresslist.AddressList
	bridgeBlockList     *addresslist.AddressList

	// journaling
	journal          *runtime.Journal
	journalRevisions []runtime.JournalRevision

	accessList *runtime.AccessList

	isL1OriginatedToken bool
}

func NewTransition(logger hclog.Logger, config chain.ForksInTime, snap Snapshot, radix *Txn) *Transition {
	return &Transition{
		logger:      logger,
		config:      config,
		state:       radix,
		snap:        snap,
		evm:         evm.NewEVM(),
		precompiles: precompiled.NewPrecompiled(),
		journal:     &runtime.Journal{},
		accessList:  runtime.NewAccessList(),
	}
}

// StorageRangeAt returns the storage at the given block height and transaction index.
func (t *Transition) StorageRangeAt(storageRangeResult *StorageRangeResult,
	addr *types.Address, keyStart []byte, maxResult int) error {
	return t.state.StorageRangeAt(storageRangeResult, addr, keyStart, maxResult)
}

func (t *Transition) WithStateOverride(override types.StateOverride) error {
	for addr, o := range override {
		if o.State != nil && o.StateDiff != nil {
			return fmt.Errorf("cannot override both state and state diff")
		}

		if o.Nonce != nil {
			t.state.SetNonce(addr, *o.Nonce)
		}

		if o.Balance != nil {
			t.state.SetBalance(addr, o.Balance)
		}

		if o.Code != nil {
			t.state.SetCode(addr, o.Code)
		}

		if o.State != nil {
			t.state.SetFullStorage(addr, o.State)
		}

		for k, v := range o.StateDiff {
			t.state.SetState(addr, k, v)
		}
	}

	return nil
}

func (t *Transition) TotalGas() uint64 {
	return t.totalGas
}

func (t *Transition) Receipts() []*types.Receipt {
	return t.receipts
}

var emptyFrom = types.Address{}

// Write writes another transaction to the executor
func (t *Transition) Write(txn *types.Transaction) error {
	if txn.From() == emptyFrom && txn.Type() != types.StateTxType {
		// Decrypt the from address
		signer := crypto.NewSigner(t.config, uint64(t.ctx.ChainID))

		from, err := signer.Sender(txn)
		if err != nil {
			return NewTransitionApplicationError(err, false)
		}

		txn.SetFrom(from)
		t.logger.Trace("[Transition.Write]", "recovered sender", from)
	}

	// Make a local copy and apply the transaction
	msg := txn.Copy()

	result, e := t.Apply(msg)
	if e != nil {
		t.logger.Error("failed to apply tx", "err", e)

		return e
	}

	t.totalGas += result.GasUsed

	logs := t.state.Logs()

	receipt := &types.Receipt{
		CumulativeGasUsed: t.totalGas,
		TransactionType:   txn.Type(),
		TxHash:            txn.Hash(),
		GasUsed:           result.GasUsed,
	}

	// The suicided accounts are set as deleted for the next iteration
	if err := t.state.CleanDeleteObjects(true); err != nil {
		return fmt.Errorf("failed to clean deleted objects: %w", err)
	}

	if result.Failed() {
		receipt.SetStatus(types.ReceiptFailed)
	} else {
		receipt.SetStatus(types.ReceiptSuccess)
	}

	// if the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(msg.From(), txn.Nonce()).Ptr()
	}

	// Set the receipt logs and create a bloom for filtering
	receipt.Logs = logs
	receipt.LogsBloom = types.CreateBloom([]*types.Receipt{receipt})
	t.receipts = append(t.receipts, receipt)

	return nil
}

// Commit commits the final result
func (t *Transition) Commit() (Snapshot, types.Hash, error) {
	objs, err := t.state.Commit(t.config.EIP155)
	if err != nil {
		return nil, types.ZeroHash, err
	}

	s2, root, err := t.snap.Commit(objs)
	if err != nil {
		return nil, types.ZeroHash, err
	}

	return s2, types.BytesToHash(root), nil
}

func (t *Transition) subGasPool(amount uint64) error {
	if t.gasPool < amount {
		return ErrBlockLimitReached
	}

	t.gasPool -= amount

	return nil
}

func (t *Transition) addGasPool(amount uint64) {
	t.gasPool += amount
}

func (t *Transition) Txn() *Txn {
	return t.state
}

// checkSenderAccount rejects transactions from senders with deployed code.
// This check is performed only in case EIP 3607 is enabled.
func (t Transition) checkSenderAccount(msg *types.Transaction) bool {
	if !t.config.EIP3607 {
		return true
	}

	codeHash := t.state.GetCodeHash(msg.From())

	return codeHash == types.ZeroHash || codeHash == types.EmptyCodeHash
}

// Apply applies a new transaction
func (t *Transition) Apply(msg *types.Transaction) (*runtime.ExecutionResult, error) {
	if !t.checkSenderAccount(msg) {
		sender := msg.From()

		return nil, fmt.Errorf("%w: address %s, codehash: %v", ErrSenderNoEOA, sender.String(),
			t.state.GetCodeHash(sender).String())
	}

	s := t.Snapshot()

	result, err := t.apply(msg)
	if err != nil {
		if revertErr := t.RevertToSnapshot(s); revertErr != nil {
			return nil, revertErr
		}
	}

	if t.PostHook != nil {
		t.PostHook(t)
	}

	return result, err
}

// ContextPtr returns reference of context
// This method is called only by test
func (t *Transition) ContextPtr() *runtime.TxContext {
	return &t.ctx
}

func (t *Transition) subGasLimitPrice(msg *types.Transaction) error {
	upfrontGasCost := new(big.Int).Mul(new(big.Int).SetUint64(msg.Gas()), msg.GetGasPrice(t.ctx.BaseFee.Uint64()))
	balanceCheck := new(big.Int).Set(upfrontGasCost)

	if msg.Type() == types.DynamicFeeTxType {
		balanceCheck.Add(balanceCheck, msg.Value())
		balanceCheck.SetUint64(msg.Gas())
		balanceCheck = balanceCheck.Mul(balanceCheck, msg.GasFeeCap())
		balanceCheck.Add(balanceCheck, msg.Value())
	}

	if have, want := t.state.GetBalance(msg.From()), balanceCheck; have.Cmp(want) < 0 {
		return fmt.Errorf("%w: address %v have %v want %v", ErrInsufficientFunds, msg.From(), have, want)
	}

	if err := t.state.SubBalance(msg.From(), upfrontGasCost); err != nil {
		if errors.Is(err, runtime.ErrNotEnoughFunds) {
			return ErrNotEnoughFundsForGas
		}

		return err
	}

	return nil
}

func (t *Transition) nonceCheck(msg *types.Transaction) error {
	currentNonce := t.state.GetNonce(msg.From())

	if msgNonce := msg.Nonce(); currentNonce < msgNonce {
		return fmt.Errorf("%w: address %v, tx: %d state: %d", ErrNonceTooHigh,
			msg.From(), msgNonce, currentNonce)
	} else if currentNonce > msgNonce {
		return fmt.Errorf("%w: address %v, tx: %d state: %d", ErrNonceTooLow,
			msg.From(), msgNonce, currentNonce)
	} else if currentNonce+1 < currentNonce {
		return fmt.Errorf("%w: address %v, nonce: %d", ErrNonceMax,
			msg.From(), currentNonce)
	}

	return nil
}

// checkDynamicFees checks correctness of the EIP-1559 feature-related fields.
// Basically, makes sure gas tip cap and gas fee cap are good for dynamic and legacy transactions
func (t *Transition) checkDynamicFees(msg *types.Transaction) error {
	if !t.config.London {
		return nil
	}

	if msg.Type() == types.DynamicFeeTxType {
		if msg.GasFeeCap().BitLen() == 0 && msg.GasTipCap().BitLen() == 0 {
			return nil
		}

		if l := msg.GasFeeCap().BitLen(); l > 256 {
			return fmt.Errorf("%w: address %v, GasFeeCap bit length: %d", ErrFeeCapVeryHigh,
				msg.From().String(), l)
		}

		if l := msg.GasTipCap().BitLen(); l > 256 {
			return fmt.Errorf("%w: address %v, GasTipCap bit length: %d", ErrTipVeryHigh,
				msg.From().String(), l)
		}

		if msg.GasFeeCap().Cmp(msg.GasTipCap()) < 0 {
			return fmt.Errorf("%w: address %v, GasTipCap: %s, GasFeeCap: %s", ErrTipAboveFeeCap,
				msg.From().String(), msg.GasTipCap(), msg.GasFeeCap())
		}
	}

	// This will panic if baseFee is nil, but basefee presence is verified
	// as part of header validation.
	if gasFeeCap := msg.GetGasFeeCap(); gasFeeCap.Cmp(t.ctx.BaseFee) < 0 {
		return fmt.Errorf("%w: address %v, GasFeeCap/GasPrice: %s, BaseFee: %s", ErrFeeCapTooLow,
			msg.From().String(), gasFeeCap, t.ctx.BaseFee)
	}

	return nil
}

// errors that can originate in the consensus rules checks of the apply method below
// surfacing of these errors reject the transaction thus not including it in the block

var (
	ErrNonceTooLow           = errors.New("nonce too low")
	ErrNonceTooHigh          = errors.New("nonce too high")
	ErrNonceMax              = errors.New("nonce has max value")
	ErrSenderNoEOA           = errors.New("sender not an eoa")
	ErrNotEnoughFundsForGas  = errors.New("not enough funds to cover gas costs")
	ErrBlockLimitReached     = errors.New("gas limit reached in the pool")
	ErrIntrinsicGasOverflow  = errors.New("overflow in intrinsic gas calculation")
	ErrNotEnoughIntrinsicGas = errors.New("not enough gas supplied for intrinsic gas costs")
	ErrInsufficientFunds     = errors.New("insufficient funds for gas * price + value")

	// ErrTipAboveFeeCap is a sanity error to ensure no one is able to specify a
	// transaction with a tip higher than the total fee cap.
	ErrTipAboveFeeCap = errors.New("max priority fee per gas higher than max fee per gas")

	// ErrTipVeryHigh is a sanity error to avoid extremely big numbers specified
	// in the tip field.
	ErrTipVeryHigh = errors.New("max priority fee per gas higher than 2^256-1")

	// ErrFeeCapVeryHigh is a sanity error to avoid extremely big numbers specified
	// in the fee cap field.
	ErrFeeCapVeryHigh = errors.New("max fee per gas higher than 2^256-1")

	// ErrFeeCapTooLow is returned if the transaction fee cap is less than the
	// the base fee of the block.
	ErrFeeCapTooLow = errors.New("max fee per gas less than block base fee")

	// ErrNonceUintOverflow is returned if uint64 overflow happens
	ErrNonceUintOverflow = errors.New("nonce uint64 overflow")
)

type TransitionApplicationError struct {
	Err           error
	IsRecoverable bool // Should the transaction be discarded, or put back in the queue.
}

func (e *TransitionApplicationError) Error() string {
	return e.Err.Error()
}

func NewTransitionApplicationError(err error, isRecoverable bool) *TransitionApplicationError {
	return &TransitionApplicationError{
		Err:           err,
		IsRecoverable: isRecoverable,
	}
}

type GasLimitReachedTransitionApplicationError struct {
	TransitionApplicationError
}

func NewGasLimitReachedTransitionApplicationError(err error) *GasLimitReachedTransitionApplicationError {
	return &GasLimitReachedTransitionApplicationError{
		*NewTransitionApplicationError(err, true),
	}
}

func (t *Transition) apply(msg *types.Transaction) (*runtime.ExecutionResult, error) {
	var err error

	if msg.Type() == types.StateTxType {
		err = checkAndProcessStateTx(msg)
	} else {
		err = checkAndProcessTx(msg, t)
	}

	if err != nil {
		return nil, err
	}

	// the amount of gas required is available in the block
	if err = t.subGasPool(msg.Gas()); err != nil {
		return nil, NewGasLimitReachedTransitionApplicationError(err)
	}

	if t.ctx.Tracer != nil {
		t.ctx.Tracer.TxStart(msg.Gas())
	}

	// 4. there is no overflow when calculating intrinsic gas
	intrinsicGasCost, err := TransactionGasCost(msg, t.config.Homestead, t.config.Istanbul)
	if err != nil {
		return nil, NewTransitionApplicationError(err, false)
	}

	// the purchased gas is enough to cover intrinsic usage
	gasLeft := msg.Gas() - intrinsicGasCost
	// because we are working with unsigned integers for gas, the `>` operator is used instead of the more intuitive `<`
	if gasLeft > msg.Gas() {
		return nil, NewTransitionApplicationError(ErrNotEnoughIntrinsicGas, false)
	}

	gasPrice := msg.GetGasPrice(t.ctx.BaseFee.Uint64())
	value := new(big.Int)

	if msg.Value() != nil {
		value = value.Set(msg.Value())
	}

	// set the specific transaction fields in the context
	t.ctx.GasPrice = types.BytesToHash(gasPrice.Bytes())
	t.ctx.Origin = msg.From()

	// set up initial access list
	initialAccessList := runtime.NewAccessList()
	if t.config.Berlin {
		// populate access list in case Berlin fork is active
		initialAccessList.PrepareAccessList(msg.From(), msg.To(), t.precompiles.Addrs, msg.AccessList())
	}

	t.accessList = initialAccessList

	var result *runtime.ExecutionResult
	if msg.IsContractCreation() {
		result = t.Create2(msg.From(), msg.Input(), value, gasLeft)
	} else {
		if err := t.state.IncrNonce(msg.From()); err != nil {
			return nil, err
		}

		result = t.Call2(msg.From(), *(msg.To()), msg.Input(), value, gasLeft)
	}

	result.AccessList = t.accessList

	refundQuotient := LegacyRefundQuotient
	if t.config.London {
		refundQuotient = LondonRefundQuotient
	}

	refund := t.state.GetRefund()
	result.UpdateGasUsed(msg.Gas(), refund, refundQuotient)

	if t.ctx.Tracer != nil {
		t.ctx.Tracer.TxEnd(result.GasLeft)
	}

	// Refund the sender
	remaining := new(big.Int).Mul(new(big.Int).SetUint64(result.GasLeft), gasPrice)
	t.state.AddBalance(msg.From(), remaining)

	// Spec: https://eips.ethereum.org/EIPS/eip-1559#specification
	// Define effective tip based on tx type.
	// We use EIP-1559 fields of the tx if the london hardfork is enabled.
	// Effective tip became to be either gas tip cap or (gas fee cap - current base fee)
	var effectiveTip *big.Int

	if t.config.London {
		effectiveTip = msg.EffectiveGasTip(t.ctx.BaseFee)
	} else {
		effectiveTip = new(big.Int).Set(gasPrice)
	}

	// Pay the coinbase fee as a miner reward using the calculated effective tip.
	coinbaseFee := new(big.Int).Mul(new(big.Int).SetUint64(result.GasUsed), effectiveTip)
	t.state.AddBalance(t.ctx.Coinbase, coinbaseFee)

	// Burn some amount if the london hardfork is applied and token is non mintable.
	// Basically, burn amount is just transferred to the current burn contract.
	if t.isL1OriginatedToken && t.config.London && msg.Type() != types.StateTxType {
		burnAmount := new(big.Int).Mul(new(big.Int).SetUint64(result.GasUsed), t.ctx.BaseFee)
		t.state.AddBalance(t.ctx.BurnContract, burnAmount)
	}

	// return gas to the pool
	t.addGasPool(result.GasLeft)

	return result, nil
}

func (t *Transition) Create2(
	caller types.Address,
	code []byte,
	value *big.Int,
	gas uint64,
) *runtime.ExecutionResult {
	address := crypto.CreateAddress(caller, t.state.GetNonce(caller))
	contract := runtime.NewContractCreation(1, caller, caller, address, value, gas, code)

	return t.applyCreate(contract, t)
}

func (t *Transition) Call2(
	caller types.Address,
	to types.Address,
	input []byte,
	value *big.Int,
	gas uint64,
) *runtime.ExecutionResult {
	c := runtime.NewContractCall(1, caller, caller, to, value, gas, t.state.GetCode(to), input)

	return t.applyCall(c, runtime.Call, t)
}

func (t *Transition) run(contract *runtime.Contract, host runtime.Host) *runtime.ExecutionResult {
	if result := t.handleAllowBlockListsUpdate(contract, host); result != nil {
		return result
	}

	// check txns access lists, allow list takes precedence over block list
	if t.txnAllowList != nil {
		if contract.Caller != contracts.SystemCaller {
			role := t.txnAllowList.GetRole(contract.Caller)
			if !role.Enabled() {
				t.logger.Debug(
					"Failing transaction. Caller is not in the transaction allowlist",
					"contract.Caller", contract.Caller,
					"contract.Address", contract.Address,
				)

				return &runtime.ExecutionResult{
					GasLeft: 0,
					Err:     runtime.ErrNotAuth,
				}
			}
		}
	} else if t.txnBlockList != nil {
		if contract.Caller != contracts.SystemCaller {
			role := t.txnBlockList.GetRole(contract.Caller)
			if role == addresslist.EnabledRole {
				t.logger.Debug(
					"Failing transaction. Caller is in the transaction blocklist",
					"contract.Caller", contract.Caller,
					"contract.Address", contract.Address,
				)

				return &runtime.ExecutionResult{
					GasLeft: 0,
					Err:     runtime.ErrNotAuth,
				}
			}
		}
	}

	// check the precompiles
	if t.precompiles.CanRun(contract, host, &t.config) {
		return t.precompiles.Run(contract, host, &t.config)
	}
	// check the evm
	if t.evm.CanRun(contract, host, &t.config) {
		return t.evm.Run(contract, host, &t.config)
	}

	return &runtime.ExecutionResult{
		Err: fmt.Errorf("runtime not found"),
	}
}

func (t *Transition) Transfer(from, to types.Address, amount *big.Int) error {
	if amount == nil {
		return nil
	}

	if err := t.state.SubBalance(from, amount); err != nil {
		if errors.Is(err, runtime.ErrNotEnoughFunds) {
			return runtime.ErrInsufficientBalance
		}

		return err
	}

	t.state.AddBalance(to, amount)

	return nil
}

func (t *Transition) applyCall(
	c *runtime.Contract,
	callType runtime.CallType,
	host runtime.Host,
) *runtime.ExecutionResult {
	if c.Depth > int(1024)+1 {
		return &runtime.ExecutionResult{
			GasLeft: c.Gas,
			Err:     runtime.ErrDepth,
		}
	}

	snapshot := t.Snapshot()
	t.state.TouchAccount(c.Address)

	if callType == runtime.Call {
		// Transfers only allowed on calls
		if err := t.Transfer(c.Caller, c.Address, c.Value); err != nil {
			return &runtime.ExecutionResult{
				GasLeft: c.Gas,
				Err:     err,
			}
		}
	}

	var result *runtime.ExecutionResult

	t.captureCallStart(c, callType)

	result = t.run(c, host)
	if result.Failed() {
		if err := t.RevertToSnapshot(snapshot); err != nil {
			return &runtime.ExecutionResult{
				GasLeft: c.Gas,
				Err:     err,
			}
		}
	}

	t.captureCallEnd(c, result)

	return result
}

func (t *Transition) hasCodeOrNonce(addr types.Address) bool {
	if t.state.GetNonce(addr) != 0 {
		return true
	}

	codeHash := t.state.GetCodeHash(addr)
	// EIP-7610 change - rejects the contract deployment if the destination has non-empty storage.
	storageRoot := t.state.GetStorageRoot(addr)

	return (codeHash != types.EmptyCodeHash && codeHash != types.ZeroHash) || // non-empty code
		(storageRoot != types.EmptyRootHash && storageRoot != types.ZeroHash) // non-empty storage
}

func (t *Transition) applyCreate(c *runtime.Contract, host runtime.Host) *runtime.ExecutionResult {
	gasLimit := c.Gas

	if c.Depth > int(1024)+1 {
		return &runtime.ExecutionResult{
			GasLeft: gasLimit,
			Err:     runtime.ErrDepth,
		}
	}

	// Increment the nonce of the caller
	if err := t.state.IncrNonce(c.Caller); err != nil {
		return &runtime.ExecutionResult{
			GasLeft: gasLimit,
			Err:     err,
		}
	}

	// we add this to the access-list before taking a snapshot. Even if the creation fails,
	// the access-list change should not be rolled back according to EIP2929 specs
	if t.config.Berlin {
		t.AddAddressToAccessList(c.Address)
	}

	// Check if there is a collision and the address already exists
	if t.hasCodeOrNonce(c.Address) {
		return &runtime.ExecutionResult{
			GasLeft: 0,
			Err:     runtime.ErrContractAddressCollision,
		}
	}

	// Take snapshot of the current state
	snapshot := t.Snapshot()

	if t.config.EIP158 {
		// Force the creation of the account
		t.state.CreateAccount(c.Address)

		if err := t.state.IncrNonce(c.Address); err != nil {
			return &runtime.ExecutionResult{Err: err}
		}
	}

	// Transfer the value
	if err := t.Transfer(c.Caller, c.Address, c.Value); err != nil {
		return &runtime.ExecutionResult{
			GasLeft: gasLimit,
			Err:     err,
		}
	}

	var result *runtime.ExecutionResult

	t.captureCallStart(c, evm.CREATE)

	defer func() {
		// pass result to be set later
		t.captureCallEnd(c, result)
	}()

	// check if contract creation allow list is enabled
	if t.deploymentAllowList != nil {
		role := t.deploymentAllowList.GetRole(c.Caller)

		if !role.Enabled() {
			t.logger.Debug(
				"Failing contract deployment. Caller is not in the deployment allowlist",
				"contract.Caller", c.Caller,
				"contract.Address", c.Address,
			)

			return &runtime.ExecutionResult{
				GasLeft: 0,
				Err:     runtime.ErrNotAuth,
			}
		}
	} else if t.deploymentBlockList != nil {
		role := t.deploymentBlockList.GetRole(c.Caller)

		if role == addresslist.EnabledRole {
			t.logger.Debug(
				"Failing contract deployment. Caller is in the deployment blocklist",
				"contract.Caller", c.Caller,
				"contract.Address", c.Address,
			)

			return &runtime.ExecutionResult{
				GasLeft: 0,
				Err:     runtime.ErrNotAuth,
			}
		}
	}

	result = t.run(c, host)
	if result.Failed() {
		if err := t.RevertToSnapshot(snapshot); err != nil {
			return &runtime.ExecutionResult{
				Err: err,
			}
		}

		return result
	}

	if t.config.EIP158 && len(result.ReturnValue) > SpuriousDragonMaxCodeSize {
		// Contract size exceeds 'SpuriousDragon' size limit
		if err := t.RevertToSnapshot(snapshot); err != nil {
			return &runtime.ExecutionResult{
				Err: err,
			}
		}

		return &runtime.ExecutionResult{
			GasLeft: 0,
			Err:     runtime.ErrMaxCodeSizeExceeded,
		}
	}

	// Reject code starting with 0xEF if EIP-3541 is enabled.
	if result.Err == nil && len(result.ReturnValue) >= 1 && result.ReturnValue[0] == 0xEF && t.config.London {
		if err := t.RevertToSnapshot(snapshot); err != nil {
			return &runtime.ExecutionResult{
				Err: err,
			}
		}

		return &runtime.ExecutionResult{
			GasLeft: 0,
			Err:     runtime.ErrInvalidCode,
		}
	}

	gasCost := uint64(len(result.ReturnValue)) * 200

	if result.GasLeft < gasCost {
		result.Err = runtime.ErrCodeStoreOutOfGas
		result.ReturnValue = nil

		// Out of gas creating the contract
		if t.config.Homestead {
			if err := t.RevertToSnapshot(snapshot); err != nil {
				return &runtime.ExecutionResult{
					Err: err,
				}
			}

			result.GasLeft = 0
		}

		return result
	}

	result.GasLeft -= gasCost
	result.Address = c.Address
	t.state.SetCode(c.Address, result.ReturnValue)

	return result
}

func (t *Transition) handleAllowBlockListsUpdate(contract *runtime.Contract,
	host runtime.Host) *runtime.ExecutionResult {
	// check contract deployment allow list (if any)
	if t.deploymentAllowList != nil && t.deploymentAllowList.Addr() == contract.CodeAddress {
		return t.deploymentAllowList.Run(contract, host, &t.config)
	}

	// check contract deployment block list (if any)
	if t.deploymentBlockList != nil && t.deploymentBlockList.Addr() == contract.CodeAddress {
		return t.deploymentBlockList.Run(contract, host, &t.config)
	}

	// check bridge allow list (if any)
	if t.bridgeAllowList != nil && t.bridgeAllowList.Addr() == contract.CodeAddress {
		return t.bridgeAllowList.Run(contract, host, &t.config)
	}

	// check bridge block list (if any)
	if t.bridgeBlockList != nil && t.bridgeBlockList.Addr() == contract.CodeAddress {
		return t.bridgeBlockList.Run(contract, host, &t.config)
	}

	// check transaction allow list (if any)
	if t.txnAllowList != nil && t.txnAllowList.Addr() == contract.CodeAddress {
		return t.txnAllowList.Run(contract, host, &t.config)
	}

	// check transaction block list (if any)
	if t.txnBlockList != nil && t.txnBlockList.Addr() == contract.CodeAddress {
		return t.txnBlockList.Run(contract, host, &t.config)
	}

	return nil
}

func (t *Transition) SetState(addr types.Address, key types.Hash, value types.Hash) {
	t.state.SetState(addr, key, value)
}

func (t *Transition) SetStorage(
	addr types.Address,
	key types.Hash,
	value types.Hash,
	config *chain.ForksInTime,
) runtime.StorageStatus {
	return t.state.SetStorage(addr, key, value, config)
}

func (t *Transition) GetTxContext() runtime.TxContext {
	return t.ctx
}

func (t *Transition) GetBlockHash(number uint64) (res types.Hash) {
	return t.getHash(number)
}

func (t *Transition) EmitLog(addr types.Address, topics []types.Hash, data []byte) {
	t.state.EmitLog(addr, topics, data)
}

func (t *Transition) GetCodeSize(addr types.Address) int {
	return t.state.GetCodeSize(addr)
}

func (t *Transition) GetCodeHash(addr types.Address) (res types.Hash) {
	return t.state.GetCodeHash(addr)
}

func (t *Transition) GetCode(addr types.Address) []byte {
	return t.state.GetCode(addr)
}

func (t *Transition) GetBalance(addr types.Address) *big.Int {
	return t.state.GetBalance(addr)
}

func (t *Transition) GetStorage(addr types.Address, key types.Hash) types.Hash {
	return t.state.GetState(addr, key)
}

func (t *Transition) AccountExists(addr types.Address) bool {
	return t.state.Exist(addr)
}

func (t *Transition) Empty(addr types.Address) bool {
	return t.state.Empty(addr)
}

func (t *Transition) GetNonce(addr types.Address) uint64 {
	return t.state.GetNonce(addr)
}

func (t *Transition) Selfdestruct(addr types.Address, beneficiary types.Address) {
	if !t.config.London && !t.state.HasSuicided(addr) {
		t.state.AddRefund(24000)
	}

	t.state.AddBalance(beneficiary, t.state.GetBalance(addr))
	t.state.Suicide(addr)
}

func (t *Transition) Callx(c *runtime.Contract, h runtime.Host) *runtime.ExecutionResult {
	if c.Type == runtime.Create {
		return t.applyCreate(c, h)
	}

	return t.applyCall(c, c.Type, h)
}

// SetNonPayable deactivates the check of tx cost against tx executor balance.
func (t *Transition) SetNonPayable(nonPayable bool) {
	t.ctx.NonPayable = nonPayable
}

// SetTracer sets tracer to the context in order to enable it
func (t *Transition) SetTracer(tracer tracer.Tracer) {
	t.ctx.Tracer = tracer
}

// GetTracer returns a tracer in context
func (t *Transition) GetTracer() runtime.VMTracer {
	return t.ctx.Tracer
}

func (t *Transition) GetRefund() uint64 {
	return t.state.GetRefund()
}

func TransactionGasCost(msg *types.Transaction, isHomestead, isIstanbul bool) (uint64, error) {
	cost := uint64(0)

	// Contract creation is only paid on the homestead fork
	if msg.IsContractCreation() && isHomestead {
		cost += TxGasContractCreation
	} else {
		cost += TxGas
	}

	payload := msg.Input()
	if len(payload) > 0 {
		zeros := uint64(0)

		for i := 0; i < len(payload); i++ {
			if payload[i] == 0 {
				zeros++
			}
		}

		nonZeros := uint64(len(payload)) - zeros
		nonZeroCost := uint64(68)

		if isIstanbul {
			nonZeroCost = 16
		}

		if (math.MaxUint64-cost)/nonZeroCost < nonZeros {
			return 0, ErrIntrinsicGasOverflow
		}

		cost += nonZeros * nonZeroCost

		if (math.MaxUint64-cost)/4 < zeros {
			return 0, ErrIntrinsicGasOverflow
		}

		cost += zeros * 4
	}

	if msg.AccessList() != nil {
		cost += uint64(len(msg.AccessList())) * TxAccessListAddressGas
		cost += uint64(msg.AccessList().StorageKeys()) * TxAccessListStorageKeyGas
	}

	return cost, nil
}

// checkAndProcessTx - first check if this message satisfies all consensus rules before
// applying the message. The rules include these clauses:
// 1. the nonce of the message caller is correct
// 2. caller has enough balance to cover transaction fee(gaslimit * gasprice * val) or fee(gasfeecap * gasprice * val)
func checkAndProcessTx(msg *types.Transaction, t *Transition) error {
	// 1. the nonce of the message caller is correct
	if err := t.nonceCheck(msg); err != nil {
		return NewTransitionApplicationError(err, true)
	}

	if !t.ctx.NonPayable {
		// 2. check dynamic fees of the transaction
		if err := t.checkDynamicFees(msg); err != nil {
			return NewTransitionApplicationError(err, true)
		}

		// 3. caller has enough balance to cover transaction
		// Skip this check if the given flag is provided.
		// It happens for eth_call and for other operations that do not change the state.
		if err := t.subGasLimitPrice(msg); err != nil {
			return NewTransitionApplicationError(err, true)
		}
	}

	return nil
}

func checkAndProcessStateTx(msg *types.Transaction) error {
	if msg.GasPrice().Cmp(big.NewInt(0)) != 0 {
		return NewTransitionApplicationError(
			errors.New("gasPrice of state transaction must be zero"),
			true,
		)
	}

	if msg.Gas() != types.StateTransactionGasLimit {
		return NewTransitionApplicationError(
			fmt.Errorf("gas of state transaction must be %d", types.StateTransactionGasLimit),
			true,
		)
	}

	if msg.From() != contracts.SystemCaller {
		return NewTransitionApplicationError(
			fmt.Errorf("state transaction sender must be %v, but got %v", contracts.SystemCaller, msg.From()),
			true,
		)
	}

	if msg.To() == nil || *(msg.To()) == types.ZeroAddress {
		return NewTransitionApplicationError(
			errors.New("to of state transaction must be specified"),
			true,
		)
	}

	return nil
}

// captureCallStart calls CallStart in Tracer if context has the tracer
func (t *Transition) captureCallStart(c *runtime.Contract, callType runtime.CallType) {
	if t.ctx.Tracer == nil {
		return
	}

	t.ctx.Tracer.CallStart(
		c.Depth,
		c.Caller,
		c.Address,
		int(callType),
		c.Gas,
		c.Value,
		c.Input,
	)
}

// captureCallEnd calls CallEnd in Tracer if context has the tracer
func (t *Transition) captureCallEnd(c *runtime.Contract, result *runtime.ExecutionResult) {
	if t.ctx.Tracer == nil {
		return
	}

	t.ctx.Tracer.CallEnd(
		c.Depth,
		result.ReturnValue,
		result.Err,
	)
}

func (t *Transition) Snapshot() int {
	snapshot := t.state.Snapshot()
	t.journalRevisions = append(t.journalRevisions, runtime.JournalRevision{ID: snapshot, Index: t.journal.Len()})

	return snapshot
}

func (t *Transition) RevertToSnapshot(snapshot int) error {
	if err := t.state.RevertToSnapshot(snapshot); err != nil {
		return err
	}

	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(t.journalRevisions), func(i int) bool {
		return t.journalRevisions[i].ID >= snapshot
	})

	if idx == len(t.journalRevisions) || t.journalRevisions[idx].ID != snapshot {
		return fmt.Errorf("journal revision id %d cannot be reverted", snapshot)
	}

	journalIndex := t.journalRevisions[idx].Index

	// Replay the journal to undo changes and remove invalidated snapshots
	t.journal.Revert(t, journalIndex)
	t.journalRevisions = t.journalRevisions[:idx]

	return nil
}

// PopulateAccessList populates access list based on the provided access list
func (t *Transition) PopulateAccessList(from types.Address, to *types.Address, acl types.TxAccessList) {
	t.accessList.PrepareAccessList(from, to, t.precompiles.Addrs, acl)
}

func (t *Transition) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	t.journal.Append(&runtime.AccessListAddSlotChange{Address: addr, Slot: slot})
	t.accessList.AddSlot(addr, slot)
}

func (t *Transition) AddAddressToAccessList(addr types.Address) {
	t.journal.Append(&runtime.AccessListAddAccountChange{Address: addr})
	t.accessList.AddAddress(addr)
}

func (t *Transition) ContainsAccessListAddress(addr types.Address) bool {
	return t.accessList.ContainsAddress(addr)
}

func (t *Transition) ContainsAccessListSlot(addr types.Address, slot types.Hash) (bool, bool) {
	return t.accessList.Contains(addr, slot)
}

func (t *Transition) DeleteAccessListAddress(addr types.Address) {
	t.accessList.DeleteAddress(addr)
}

func (t *Transition) DeleteAccessListSlot(addr types.Address, slot types.Hash) {
	t.accessList.DeleteSlot(addr, slot)
}
