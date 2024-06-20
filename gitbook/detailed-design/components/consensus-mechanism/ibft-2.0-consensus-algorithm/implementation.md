# Implementation

In the diagram below, the key components of IBFT consensus mechanism are shown.

<figure><img src="../../../../.gitbook/assets/polybft_implementation_improvement.png" alt=""><figcaption><p>Components of IBFT Consensus Mechanism</p></figcaption></figure>

The `Message` component is responsible for storing messages received from other participants in the network. With the help of these messages, `IBFT` reaches consensus for a new block.&#x20;

The `Backend` component is responsible for providing all additional functionalities and the data that `IBFT` requires in the current sequence.&#x20;

The `ValidatorManager` component contains data related to quorum and voting power. Quorum is determined by the formula (2 \* totalVotingPower / 3) + 1, where totalVotingPower represents the influence each validator has in the decision-making process.&#x20;

The `Transport` component, as the name suggests, is used for sending messages to other nodes.

The next section explains the consensus backend.
