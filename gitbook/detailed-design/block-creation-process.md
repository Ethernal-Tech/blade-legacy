---
description: >-
  In this section block creation process is described together with all related
  components.
---

# Block Creation Process

Block creation is the essence of the blockchain system. The following figure shows all the components involved in the process (for clarity reasons, some fields and methods in the structures are omitted):

<figure><img src="../.gitbook/assets/polybft_block_creation.png" alt=""><figcaption><p>Elements of the Block Creation Process</p></figcaption></figure>

The `BlockBuilder` represents one of the most important components of the system, which, as the name suggests, enables the creation of a block. This component possesses specific configuration parameters (`BlockBuilderParams`), whose values are set during its instantiation by calling the `NewBlockBuilder` method. The key parameters and their meanings are outlined below:

1. **`parent`** - header of the previous (parent) block.
2. **`executor`** - auxiliary component invoked by `BlockBuilder` during block creation.
3. **`gasLimit`** - maximum amount of gas in the block.
4. **`blockTime`** - maximum time for block creation.
5. **`txPool`** - reference to the transaction pool from which transactions are selected.
6. **`baseFee`**

In addition to these immutable parameters during block creation, there are specific fields in the `BlockBuilder` structure that reflect the current state of the block being created. The content of these fields changes as the block creation process progresses. These include:

1. **`header`** - the block header,
2. **`txns`** - a list of selected transactions for the block,
3. **`block`** - a reference to the block itself (set only after the block is completely created), and
4. **`state`** - an additional auxiliary component (a.k.a.`Transition`) enabling the transition from one state to the next during block creation.

To fully grasp what follows, it is crucial to understand that block creation is based on state changes. The initial state is the last state from the previous block. Transitioning to the next state occurs when selecting a new valid transaction to include in the block. Therefore, the number of transactions in the block determine how many state transitions are necessary. The previously mentioned `Transition` component is responsible for verifying the validity of the transaction and, if everything is correct, transitioning to the next state.

The `BlockBuilder` has several methods. Below, each method is explained in sequence, illustrating how they impact the block creation process. The order in which they are mentioned reflects the sequence in which they must be called during block creation. The methods are as follows:

1. **`NewBlockBuilder`**
2. **`Reset`**
3. **`Fill`**
4. **`WriteTx`**
5. **`Build`**

### `Reset`

The first method to call after creating a `BlockBuilder` is `Reset`. This method is responsible for setting all fields of the `BlockBuilder` related to the currently created block to their initial values. The set of transactions chosen for the block is set to an empty list, the header is filled with values that can be calculated at the given moment, the block reference does not exist (set to nil), while the `transition` object, using the `Executor` parameter, is created with all elements necessary for proper transaction validation and potential transition to the next state. Some of these elements include the total gas amount in the block, allow lists, currently used gas (set to 0), reference to EVM, fork-related information, initial state (the last state from the previous block), etc.

<figure><img src="../.gitbook/assets/polybft_block_creation_sequence.png" alt=""><figcaption><p>Block Creation Sequence Diagram</p></figcaption></figure>

### Fill

Once the initial state is set, the next method to call is `Fill`. It takes one transaction at a time from the transaction pool, checks its validity, and if everything is fine, records it in the list of selected transactions for the given block. It is important to understand that at this point, the transaction is not yet placed in the block but only entered into the list of selected transactions. There are two possible conditions for completing the execution of this method:

* the timer expires, i.e., the time allotted for block creation ends, or
* the sum of gas for all selected transactions exceeds the maximum gas limit that can be in the block.

Upon calling the `Fill` method, the timer is started, and the `Prepare` method of the transaction pool is invoked. This method allows preparing the transaction pool by sorting transactions by their fee. The next step is initiating an infinite for loop where, in each iteration, by calling the `Peek` method of the transaction pool, one transaction is taken. The transaction is then validated, and if everything is fine, a state change is executed, and the transaction is recorded in the list of selected transactions. This is achieved by calling the third method of `BlockBuilder` (`WriteTx`) for each transaction. If everything goes well, the transaction is removed from the transaction pool using the `Pop` method, and the loop proceeds to the next iteration (if possible). Exiting the for loop is accomplished by satisfying one of the previously mentioned two conditions.

### WriteTx

The `WriteTx` method of `BlockBuilder` is invoked for each transaction that may potentially be chosen for later inclusion in the block. Whether it will be chosen or not depends on its validity. The validity is checked using the `Write` method of the `transition` object. In other words, for each transaction from the `Fill` method, the `WriteTx` method of `BlockBuilder` is called first, and then from `WriteTx`, the `Write` method of the `transition` object is called. If everything is correct in the `Write` method, the transaction is valid, and within it, the transition to the next state occurs. Upon returning to `WriteTx`, with the condition that everything is correct, the transaction is recorded in the list of selected transactions. An invalid transaction will not lead to the interruption of block creation; instead, it is discarded, and the process remains in the current state.

> The execution of the transition (transaction validation and transition to the next state), or the Write method, represents the core of the new block creation process.

The first thing accomplished within the `Write` method is taking a Snapshot of the current state. This allows for a safe return to the previous, secure state in case the transition fails (i.e., unsuccessful processing of a given transaction). After taking the Snapshot, the process of transaction validation and gradual transition to the next state begins. The procedure is as follows:

1. Determine the type of transaction and, depending on that, perform different checks.
   1. Checks differ based on whether it is (i) a state transaction or (ii) a legacy transaction / dynamic fee transaction.
   2. The most crucial check is related to gas and sender's funds.
   3. State transactions must not have any gas spending set.
   4. Legacy and dynamic fee transactions must be configured so that the sender has enough funds to pay the maximum gas defined in the transaction (in other words, the sender must have funds to pay the maximum gas even if he would not spend it all).
   5. If the sender has enough funds, the amount for the maximum gas is deducted; otherwise, the transaction is invalid, and further steps are not taken.
2. Subtract the maximum gas amount of the transaction from the total gas that can appear in the block.
   1. If the result of this calculation is a negative number, the transaction is rejected, and further steps are not taken.
   2. This simultaneously satisfies one of the conditions for completing the `Fill` method.
3. Calculate the initial, basic, guaranteed gas consumption (intrinsic gas) that depends on whether it is a transaction creating a smart contract, calling a smart contract, or transferring funds.
4. Check if this gas amount is less than the maximum set in the transaction (max gas).
   1. If not, the transaction is invalid, and further steps are not taken.
5. If it is not a transaction just transferring funds, call the constructor or function of the smart contract, consuming additional gas (must be less than max gas - intrinsic gas).
6. Since funds were initially deducted from the sender's balance as if spending the maximum gas, at this point, funds for the remaining gas are returned.
7. Payment of potential fees associated with dynamic transactions.
8. Since at the beginning, the maximum gas in the transaction was deducted from the maximum gas that can be in the block, at this point, the remaining gas is returned to that gas pool.

Upon completion of step 8, the transaction is marked as valid, simultaneously completing the transition to the next state. As a result, a receipt is generated. Together with other receipts, it is later added to the block header and serves as confirmation that the transaction execution occurred without errors. Finally, upon returning to the `WriteTx` method, the transaction is added to the list of transactions selected for inclusion in the block.

### Build

The last method of `BlockBuilder` to be called is `Build`. Its role is to create a concrete full block based on the content of previously configured fields in `BlockBuilder`. Within this method, the previously selected transactions are inserted into the block, the Merkle Tree Root is calculated for transactions, receipts, and transition states, they are all added to the block header, etc.
