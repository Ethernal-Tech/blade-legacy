# IBFT Implementation

We won't delve into the detailed implementation of IBFT 2.0 in this paper, as it is beyond the scope of our focus. However, in the diagram below, we can observe the key components of IBFT.

![](<../../../.gitbook/assets/13 (1).png>)

5.3.1 Components of IBFT consensus mechanism

The Message component is responsible for storing messages received from other participants in the network. With the help of these messages, IBFT reaches consensus for a new block. The Backend component is responsible for providing all additional functionalities and the data that IBFT requires in the current sequence. The ValidatorManager component contains data related to quorum and voting power. Quorum is determined by the formula (2 \* totalVotingPower / 3) + 1, where votingPower represents the influence each validator has in the decision-making process. The Transport component, as the name suggests, is used for sending messages to other nodes.
