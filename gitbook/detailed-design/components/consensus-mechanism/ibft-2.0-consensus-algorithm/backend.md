---
description: >-
  This chapter gives a detailed description of how IBFT utilizes the IBFT
  backend component to successfully reach consensus for a new block to be added
  to the blockchain.
---

# Backend

&#x20;It is widely known that every blockchain starts with a genesis block that is identical for all participants, as is the case in our solution. Each subsequent block is added to the blockchain using a consensus algorithm, where a set of validators determines the fate of the block, and the data used by selected validators to add the block varies. To ensure that IBFT has access to the necessary data at all times, for creation of a new block, it leverages the IBFT backend component.

As already described in [IBFT 2.0 Initialization](initialization.md), IBFT is initiated by invoking the `RunSequence()` method and is implemented as a state machine. The initial state it starts in is 'newRound.' In the initial state (newRound), IBFT checks whether the current node is the block proposer for the given round and height. If the current node is chosen to be the proposer for the current round and height, it means it needs to create a new block. The creation of a new block is implemented in the `IBFT Backend,` and `IBFT` will call the `BuildProposal()` method from the `IBFT Backend`, which returns the created block.

To ensure that the created block is known to other validators, the chosen proposer creates a PrePrepare message through which the block is sent to other validators. The creation of the PrePrepare message is also done in the `IBFT Backend` in the `BuildPrePrepareMessage()` method.&#x20;

After creating the PrePrepare message, the proposer marks the new block as an accepted proposal, while other validators participating in the consensus must perform certain validations on the block before marking it as an accepted proposal. The first method called from the `IBFT Backend` to verify the proposal block is `IsProposer()`. This method checks whether the sender of the block is indeed the chosen proposer for the current round and height. After that, the `IsValidProposalHash()` method is called, which verifies whether the hash in the message matches the hash of the proposed block. To validate that the proposed block is correct, i.e., that the transactions in the block are valid, the `IsValidProposal()` method is called. This method executes all transactions and decides whether the sent block is valid. In the case that the round is greater than zero, during the verification of RoundChange messages, the `IsValidValidator()` method is called from the `IBFT Backend` to check whether the sender of the Round Change Certificate is valid. The last method called from the `IBFT Backend` before `IBFT` transitions to a new state is `BuildPrepareMessage()`. After sending the Prepare message, `IBFT` transitions to the ‘prepare’ state.

In the prepare state, a validator retrieves all Prepare messages from its `Message` storage for the corresponding height and round. Subsequently, it performs validation on these messages. For each message, the `IsValidProposalHash()` method is called from the `IBFT Backend` to verify whether the hash in the Prepare message matches the Proposal hash. If a sufficient number of Prepare messages exist, satisfying the quorum requirement, a Commit message is sent. After sending the message, `IBFT` transitions to a new state called ‘Commit’.

Similarly to how `IBFT` in the Prepare state collected all Prepare messages from the `Message` storage, it now does the same but specifically collects Commit messages for a given round and height. Each message must undergo validation, and only valid messages are considered when checking the quorum for Commit messages. For each message, the first step is to verify whether the hash in the Commit message matches the Proposal hash. This verification is done by calling the `IsValidProposalHash()` method from the `IBFT Backend`. After this verification, the next step is to check whether the signature of the proposal hash in the Committed Seal is indeed signed by the validator. Once the quorum for Commit messages is reached, `IBFT` transitions to the Final state.

In the ‘Final’ state, IBFT adds a new block to its local blockchain and removes all messages from its Message storage that were used for previous decisions on the correctness of the block. The block is added to the blockchain by invoking the `InsertProposal()` method from the `IBFT Backend.` After successfully adding the new block to the blockchain, `IBFT` sends a signal on the roundDone channel, thus halting the execution of the initiated sequence.

![Sequence diagram of where IBFT calls the IBFT backend](<../../../../.gitbook/assets/14 (1).png>)

