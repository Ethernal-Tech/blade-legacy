# Initialization

The starting point of Blade's consensus algorithm is the `Polybft` component, which serves as a wrapper around `IBFT`. The `Polybft` component is instantiated only once, during node startup, and remains unchanged throughout the node's operation until it is shut down. Two additional components that also remain unchanged during the node startup are `IBFT` and `ConsensusRuntime` (see _Components of Consensus Mechanism_ sequence diagram).

<figure><img src="../../../../.gitbook/assets/polybft_initialization_improvement.png" alt=""><figcaption><p>Components of Consensus Mechanism</p></figcaption></figure>

`Polybft`, through the `Initialize()` method, sets its initial data and simultaneously invokes the appropriate methods to create `IBFT` and `ConsensusRuntime`, and stores their instances within the previously instantiated `Polybft`.

1. The `ConsensusRuntime` is created by calling the `newConsensusRuntime()` method. The role of the `ConsensusRuntime` object is to create a new instance of the `IBFT` backend for each new initiation of the IBFT consensus algorithm.
2. `Polybft` uses the `newIBFT()` method to create `IBFT`, with aim to achieve consensus for a new block.

When a new IBFT sequence is initiated, `ConsensusRuntime` is tasked with creating a new instance of `IBFTBackend` by invoking the `CreateIBFTBackend()` method.&#x20;

In the sequence diagram below, we can see that when the current node is a validator of the current block, it uses the `createIBFTBackend()`method to create the backend for `IBFT`. After creating the `IBFT` backend, `Polybft` sets the created backend in `IBFT` using the `setIBFTBackend()` method. If all the previous steps are successfully executed, `Polybft` initiates the IBFT consensus mechanism by calling the `RunSequence()` method and receives a `sequenceCh` chain in response, on which it listens for the completion of creating a new block.

<figure><img src="../../../../.gitbook/assets/polybft_initialization_sequence.png" alt=""><figcaption><p>Sequence diagram for IBFT backend creation</p></figcaption></figure>

In the next section we define the used `IBFT` model.

