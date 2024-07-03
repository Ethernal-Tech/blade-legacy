# Networking

The `PolyBFT` component utilizes a decentralized networking layer based on the `libp2p` protocol. This protocol offers peer-to-peer networking features, including peer discovery, connection management, and secure messaging.

### Peer discovery

`Polybft` leverages `libp2p`'s distributed hash table (DHT) for peer discovery. The DHT is based on the Kademlia algorithm. It stores information about other peers in the network, including their addresses and availability. When a new node joins the network, it uses the DHT to find peer nodes that are currently online. This process of using the DHT for peer discovery and sending out connection requests is repeated periodically to maintain a sufficient number of connections within the network.

### Peer routing

Bootnodes (nodes that must exist in the network, i.e the network entry points) function as rendezvous servers, helping new nodes in discovering and connecting to the network. One or more bootnodes can be specified when the genesis file is created. Bootnodes are defined using libp2p multiaddrs, which include information about the protocol, network address, and node port number.

### Gossipsub

Gossipsub is a decentralized, peer-to-peer messaging protocol utilized in Blade to broadcast messages efficiently across the network. It is employed in various network components, including the `TxPool`, where it is used to broadcast new transactions and relay transaction data between nodes. Gossipsub enables efficient and reliable message propagation while minimizing the network's bandwidth requirements.

For more information about libp2p networking, explore the official [libp2p](https://docs.libp2p.io/) documentation.
