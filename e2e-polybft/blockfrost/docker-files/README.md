# Apex vector testnet relay

## Prerequisites:

* intel based linux system (this was tested on)
* docker with compose
* network

## Start procedure

Run:

```
docker compose up -d
```

To check the tip (at the moment it is about 10 min to sync, will definitely vary over time):

```
docker exec -it vector2-testnet-apex-relay-1 cardano-cli query tip --testnet-magic 1177 --socket-path /ipc/node.socket
```

To check the blockfrost point a browser to `localhost` port `3000`, for example:

```
http://localhost:3000
http://localhost:3000/epochs/latest
```

## Remove procedure

To remove containers and volumes, images will be left for fast restart:

```
docker compose down
docker volume rm vector2-testnet_db-sync-data vector2-testnet_node-db vector2-testnet_node-ipc vector2-testnet_postgres
```
