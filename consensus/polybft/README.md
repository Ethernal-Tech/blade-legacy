
# Polybft consensus protocol

Polybft is a consensus protocol, which runs [go-ibft](https://github.com/0xPolygon/go-ibft) consensus engine.

It has a native support for running bridge, which enables running cross-chain transactions with Ethereum-compatible blockchains.

In the following text, we will explain how to start a blade chain in different configurations.

## Initial steps

1. Build binary

    ```bash
    $ go build -o blade .
    ```

2. Init validator secrets - this command is used to generate account secrets (ECDSA, BLS as well as P2P networking node id). `--data-dir` denotes folder prefix names and `--num` how many accounts need to be created. **This command is for testing purposes only.**

    ```bash
    $ blade secrets init --data-dir test-chain- --num 4
    ```

3. Genesis -  this command creates chain configuration, which is needed to run a blockchain, like `block-gas-limit`, `block-time`, `epoch-size`, `blade-admin`, `epoch-reward`, native token configuration, etc. It contains initial validator set as well and there are two ways to specify it:
   - all the validators information are present in local storage of single host and therefore directory if provided using `--validators-path` flag and validators folder prefix names using `--validators-prefix` flag
   - validators information are scafollded on multiple hosts and therefore necessary information are supplied using `--validators` flag. Validator information needs to be supplied in the strictly following format:
   `<multi address>:<public ECDSA address>:<public BLS key>:<BLS signature>`.
    **Note:** when specifying validators via validators flag, entire multi address must be specified.

    ```bash
    $ blade genesis --block-gas-limit 10000000 --epoch-size 10 \
        --proxy-contracts-admin 0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed \
        [--validators-path ./] [--validators-prefix test-chain-] \
        [--consensus polybft] \
    ```

## Blade as L1 (without Bridge)
Just run blade cluster (start each node defined in the initial validator set). For example:

    ```bash
    $ blade server --data-dir ./test-chain-1 --chain genesis.json --grpc-address :5001 --libp2p :30301 --jsonrpc :9545 \
    --seal --log-level DEBUG

    $ blade server --data-dir ./test-chain-2 --chain genesis.json --grpc-address :5002 --libp2p :30302 --jsonrpc :10002 \
    --seal --log-level DEBUG

    $ blade server --data-dir ./test-chain-3 --chain genesis.json --grpc-address :5003 --libp2p :30303 --jsonrpc :10003 \
    --seal --log-level DEBUG
    
    $ blade server --data-dir ./test-chain-4 --chain genesis.json --grpc-address :5004 --libp2p :30304 --jsonrpc :10004 \
    --seal --log-level DEBUG
    ```

    It is possible to run nodes in "relayer" mode. It allows automatic execution of deposit and withdrawal events on behalf of users.
    In order to start node in relayer mode, it is necessary to supply the `--relayer` flag:

    ```bash
    $ blade server --data-dir ./test-chain-1 --chain genesis.json --grpc-address :5001 --libp2p :30301 --jsonrpc :9545 \
    --seal --log-level DEBUG --relayer
    ```

## Run Blade as L2 (with Bridge)
1. Start rootchain server - rootchain server is a Geth instance running in dev mode, which simulates Ethereum network. **This command is for testing purposes only.**

    ```bash
    $ blade bridge server
    ```

2. Deploy and initialize rootchain contracts - this command deploys rootchain smart contracts and initializes them. It also updates genesis configuration with rootchain contract addresses and rootchain default sender address.

    ```bash
    $ blade bridge deploy \
    --deployer-key <hex_encoded_rootchain_account_private_key> \
    --proxy-contracts-admin 0xaddressOfProxyContractsAdmin \
    [--genesis ./genesis.json] \
    [--json-rpc http://127.0.0.1:8545] \
    [--test]
    ```
3. Fund validators on rootchain - in order for validators to be able to send transactions to Ethereum, they need to be funded in order to be able to cover gas cost on L1. **This command is for testing purposes only.**

    ```bash
    $ blade bridge fund \
        --addresses 0x1234567890123456789012345678901234567890 \
        --amounts 200000000000000000000
    ```

4. Do mint and premine for relayer node. **These commands should only be executed if non-mintable (L1 originated) native token is used**

    ```bash
    $ blade mint-erc20 \ 
    --erc20-token <address_of_native_root_erc20_token> \
    --private-key <hex_encoded_private_key_of_token_deployer> \
    --addresses <address_of_relayer_node> \
    --amounts <ammount_of_tokens_to_mint_to_relayer>
    ```

     ```bash
    $ blade rootchain premine \ 
       --erc20-token <address_of_native_root_erc20_token> \
       --blade-manager <address_of_BladeManager_contract_on_root> \
       --private-key <hex_encoded_private_key_of_relayer_node> \
       --premine-amount <ammount_of_tokens_to_premine>
       --stake-amount <ammount_of_tokens_relayer_staked_on_blade>
    ```

5. Finalize bridge setup on rootchain (`BladeManager`) contract. This is done after all addresses we need premined on rootchain, and it's a final step that is required before starting the child chain. This needs to be done by the deployer of BladeManager contract (the user that run the deploy command). He can use either its hex encoded private key, or data-dir flag if he has secerets initialized. The commands says to rootchain that tokens of premined addresses will be locked on L1, so that they can be used from L2 start. **This command should only be executed if non-mintable (L1 originated) erc20 native token is used**

    ```bash
    $ blade bridge finalize-bridge --private-key <hex_encoded_rootchain_account_private_key_of_supernetManager_deployer> \
    --genesis <path_to_genesis_file> \
    --blade-manager <address_of_BladeManager_contract> \
    ```
6. Run validator and relayer nodes like in chapter `Blade as L1 (without Bridge)`.
