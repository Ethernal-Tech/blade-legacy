# IBFT Initialization

The starting point of our consensus algorithm is the Polybft component, which serves as a wrapper around IBFT. The Polybft component is instantiated only once during node startup and remains unchanged throughout the node's operation until it is shut down. Two additional components that also remain unchanged throughout a single node startup are IBFT and ConsensusRuntime.

![](<../../../.gitbook/assets/11 (1).png>)

5.2.1. Components of consensus mechanism

Polybft will, through the Initialize() method, set its initial data and simultaneously invoke the appropriate methods to create IBFT and ConsensusRuntime, and store their instances within the previously instantiated Polybft.

1. The ConsensusRuntime is created by calling the newConsensusRuntime() method. The role of the ConsensusRuntime object is to create a new instance of the IBFT backend for each new initiation of the IBFT consensus algorithm.
2. Polybft will use the newIBFT() method to create IBFT, tasked with achieving consensus for a new block.

When a new IBFT sequence is initiated, ConsensusRuntime is tasked with creating a new instance of IBFTBackend by invoking the CreateIBFTBackend() method. Below in the sequence diagram we can see, if the current node is a validator for the current block, it will use the createIBFTBackend() method to create the backend for IBFT. After creating the IBFT backend, Polybft sets the created backend in IBFT using the setBackend() method. If all the previous steps are successfully executed, Polybft initiates the IBFT consensus mechanism by calling the RunSequence() method and receives a sequenceCh in response, on which it listens for the completion of creating a new block.

![](<../../../.gitbook/assets/12 (1).png>)

5.2.2 Sequence diagram for IBFT backend creation

In the next section we define the used IBFT model.

