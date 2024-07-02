---
description: >-
  This section gives an overview of the Ethernal Blade blockchain high-level
  architecture.
---

# High-level Architecture

To better understand the Blade system, the high-level architecture diagram of components is illustrated below. A node in the diagram is any instance of Blade software connected to other computers running Blade software, forming a network. We also refer to this node as the Blade client or client for short.

The client exposes predefined RPC methods through a set of APIs (i.e., gRPC, JSON-RPC within the `RPC` component). Among other things, this enables the sending of transactions to the blockchain and the reading of current blockchain data.

Sent transactions are submitted to the transaction pool (represented by `TxPool` component). For reading a block from the blockchain, a transaction from a block, or the current state of an account, a request is submitted to the `Storage` component. The `Storage` component is a general abstraction for different types of storage holding various data like the blockchain itself, the world state, bridge data, etc.

The transaction pool prioritizes received transactions, usually by giving an advantage to transactions with higher gas prices, but the applied criteria may differ. Each received transaction from the transaction pool is sent to the rest of the nodes/peers in peer-to-peer network using the `LibP2P` communication component. This component uses gossip mechanism to spread received messages through the network.

If a client node is in the role of a validator, it takes part in the block creation process, and the consensus mechanism is an integral part of its operational behavior. The consensus mechanism, which is essentially an algorithm using state transitions based on the received confirmations, helps validators come to an agreement regarding the validity of a proposed block. The `Consensus` component (the algorithm) is supported by the `Consensus Backend` component, a complex component, comprised of several other components.&#x20;

As the algorithm assumes that only one validator from all consensus participants/validators is a block proposer, the `Consensus Backend` also holds the logic to determine whether the validator is a proposer or not. For example, when the consensus algorithm starts, it checks the proposer status of the validator, and if the status is true, it calls the `Block Management` component, which takes transactions from the `TxPool` to form a proposed block. The consensus relies on a number of messages (such as PREPREPARE, PREPARE, COMMIT, ROUND-CHANGE, FINAL) during different stages of the block creation process. These messages are created by the `Consensus Backend` as well. As all validators equally participate in the consensus, the messages have to be distributed to all of them. The communication layer of the `Consensus Backend` ensures that all messages are sent to all peers through `LibP2P`. Messages work in pub/sub manner meaning that only subscribed nodes receive messages for the given topic.&#x20;

Once all messages are exchanged through different consensus phases, and assuming that consensus is reached (meaning that the proposed block is valid and ready to be added to the blockchain), the next step is to apply all block transactions and add the block.&#x20;

The `Block Management` component takes the block and sends each transaction for execution. The `Executor` component determines whether it is a smart contract call or an external owned account (EOA) balance change, and applies them accordingly. In the case of a smart contract, the contract is retrieved from storage and sent to the `EVM` (Ethereum Virtual Machine) for execution. Consequently, the `World State` is updated by the `EVM`, adjusting the balance of the contract account and its storage. For EOAs, the `World State` is directly updated by adjusting the balance of the account. After all transactions are executed and the `World State` is updated, the block's header is also updated with the latest `World State` hash and the block is appended to the blockchain (saved in `Blocks` storage).

The block is also sent to all peers using `Syncer` component.

<figure><img src="../.gitbook/assets/system_architecture-high-level arch.drawio(4).png" alt=""><figcaption><p>High-level Architecture Component Diagram</p></figcaption></figure>
