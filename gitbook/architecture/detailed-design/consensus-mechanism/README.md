# Consensus Mechanism

In order to enable the transition to the next state within a system consisting of an unbounded number of asynchronous nodes, it is necessary to establish a mechanism for coordinating all network participants around a set of changes to be applied. For this reason, when creating a new blockchain network, one of the most important consensus rules to be defined is how participants in the network reach an agreement on the block that will be accepted as the next one in the chain. Despite the presence of various widely accepted solutions in the contemporary blockchain networks, as the creators of the Blade system, we have not found the one that would satisfy all our requirements in any of them. Analyzing just the two most dominant networks, Nakamoto's (Bitcoin) Proof-of-Work (PoW) approach, while the most secure, requires a significant amount of electrical energy and has relatively lower throughput, while the decentralization of Ethereum's Proof-of-Stake (PoS) consensus algorithm is quite questionable. Our goal was to strike a balance between these two approaches. Therefore, one of the Proof-of-Authority (PoA) solutions was adopted - the IBFT (Istanbul Byzantine Fault Tolerant) 2.0 consensus algorithm.

Leveraging the IBFT 2.0 consensus algorithm not only enables us to establish a system with a commendable balance of security and throughput but also achieves this at a remarkably low energy cost. Furthermore, the introduction of a dynamic set of validators, determined through a voting mechanism, facilitates the desired decentralization. A critical prerequisite for the successful functioning of IBFT 2.0, as well as all other Byzantine Fault Tolerant (BFT) algorithms (as definition by Leslie Lamport) operating in a partially synchronized state, is that the total number of validator nodes engaged in the decision-making process must be at least **3f+1**, where **f** signifies the quantity of malicious participants. The security profile in these algorithms is characterized by extremes. To elaborate, fulfillment of the mentioned condition maximizes security, whereas failure to meet it results in a complete collapse of security.

![](<../../../.gitbook/assets/2 (1).png>)

5.0.1. Failure probability of IBFT 2.0 consensus algorithm

The key assumption underpinning our system is its operation within the realm of partial synchrony. Specifically, it means there is a point in time (GST) after which the delay in message delivery (latency) becomes bounded by a finite and constant value. This assumption is fundamental to the previously defined **3f+1** security model of the system. For further details, please refer to the work "_Consensus in the Presence of Partial Synchrony_" by Cynthia Dwork, Nancy Lynch and Larry Stockmeyer.

The **Immediate Finality** stands out as a pivotal feature of the IBFT 2.0 consensus algorithm, which significantly contributed to our decision to employ this protocol. This attribute denotes that once a transaction is included in a block becoming part of the chain, its position is guaranteed and will not change unless the security assumptions of the system are compromised. For instance, one such assumption is that at no point in time the number of Byzantine nodes exceeds two-thirds majority, as any such occurrence would enable them to completely overwrite the remainder of the blockchain.

**Byzantine fault tolerant (BFT).** Byzantine Fault Tolerance (BFT) defines a class of consensus algorithms resistant to cases where a specific set of nodes exhibits arbitrary malicious behavior. Leslie Lamport was the first to introduce the problem and the method of achieving agreement among honest nodes in the presence of these malicious (Byzantine) participants. He presented the problem through the concept of achieving interactive consistency, ensuring that all correct nodes perceive the rest of the network in the same way. Importantly, all security conclusions were initially drawn under the assumption of a fully (mostly unrealistically) synchronous environment. Cynthia Dwork and her collaborators later revisited this problem, defining security constraints in a much more realistic scenario of partial synchrony. The Byzantine failure mode represents the most robust known failure mode in the realm of consensus algorithms. For more details on interactive consistency, achieved security conclusions and the concept of Byzantine nodes, please refer to the following works:

1. "_Reaching Agreement in the Presence of Faults_" M. Pease, R. Shostak, L. Lamport
2. "_The Byzantine Generals Problem_" L. Lamport, R. Shostak, M. Pease

**Eventually synchronous network.** The way nodes are synchronized in a network from a communication perspective is crucial for the security of the system. Depending on the assumptions made regarding transmission latency, there are three network models:

1. Synchronous network - the maximum latency is bounded and known
2. Asynchronous network - the maximum latency is unknown and messages may never be delivered
3. Partially synchronous network (C. Dwork, N. Lynch and L. Stockmeyer) - two types:
   1. Messages delivery is guaranteed, but the latency, although finite, is unknown
   2. Eventually synchronous network - there is a point in time known as the global stabilization time (GST) after which the message delay is bounded by a finite and constant value

While the synchronous model represents the most optimal and dependable approach, its realization proves challenging in practical scenarios, particularly within distributed decentralized systems like blockchain networks. Conversely, the asynchronous model stands as unacceptable, a fact substantiated by Fischer's seminal work, "_Impossibility of Distributed Consensus with One Faulty Process_". In such contexts, achieving consensus becomes an insurmountable challenge, even with the presence of just one fail-stop node. Hence, the prevailing and pragmatic assumption is that of eventual synchronization, wherein the network becomes synchronous only after a designated point in time, known as the Global Stabilization Time (GST). IBFT 2.0, like most consensus algorithms, is based on such an assumption. For more detail on this type of synchronization please refer to the work "_Consensus in the Presence of Partial Synchrony_" by Cynthia Dwork, Nancy Lynch and Larry Stockmeyer. The following table shows the security prerequisites for different types of synchronization.

![](<../../../.gitbook/assets/3 (1).png>)

5.0.2. Smallest number of nodes for which t(n) - resilient consensus protocol exists

("_Consensus in the Presence of Partial Synchrony_", page 4 (291), table 1)

**Immediate Finality.** Immediate finality denotes a unique aspect of consensus protocols in which, once a transaction is included in a selected block, it becomes irreversible as well as its position remains unchanged. This stands in contrast to the probabilistic finality approach seen in Bitcoin and Ethereum, where the probability of a transaction being involved in a reorganization decreases with its depth in the chain. It's crucial to emphasize that this irreversibility is conditional on the preservation of the protocol's security requirements. IBFT 2.0 boasts the immediate finality property.

The sections below are organized as follows. In Section 5.1 we delve into the core principles that govern the functionality of the IBFT 2.0 consensus algorithm. In Section 5.2 we describe the initialization process of consensus components. In Section 5.3 we define the used IBFT model and Section 5.4 explains the consensus backend.
