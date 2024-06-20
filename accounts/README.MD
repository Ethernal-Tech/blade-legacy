Account management
===

## Table of Contents

[TOC]

## Problem
Account management is a feature that implements the logic for storing private keys for non-validator users. These private keys are used for signing various types of data. Currently, we have implemented file storage on the local disk. The keys are stored in a text file in JSON format. To ensure security, the private keys are encrypted, so opening the file does not compromise sensitive data.

## Account storage
Account storage is component of account management which is used to manage keys file. When system starts it can create a new file if neccessary or load account from existing file.Account cache adds, delete or update accounts in file. Its primary purpose is to keep the key file updated with the latest account data.

## Key storage
Keys are stored in a file in JSON format. All keys are stored in the same file. The data structure used to marshal the keys is map[Address]EncryptedKey, making it very easy to retrieve a keys from the file while keeping the sensitive data secure.

## Keystore
The keystore is a layer between the AccountManager and the AccountStore, where cryptographic operations take place. The keystore maintains its own list of wallets (wrappers around accounts) and notifies the account manager about any changes (such as accounts being added or deleted). In this layer, transactions and data are signed, and private keys are kept unlocked for actions that require an unlocked private key (such as eth_sign).

## Account manager
The account manager is the top layer, and the system interacts with it for every account management task. It handles all types of key storage (currently, we only support local keystore storage) and merges all data from different storage types into one list.

## Interacting with account management
Accounts are required for some features. So our software support two diffent ways of interaction with account management. You can interact with commands or with different json rpc calls.

### Suported json-rpc calls
* **personal_listAccount** - return addresses of all accounts
* **personal_newAccount** - create new account and return address of account
* **personal_importRawKey** - insert hex raw key and storing them in storage
* **personal_unlockAccount** - unlockes account so that can sign transaction with account private key, it used for eth_sign and other calls that doesn't send password for decrypt private key
* **personal_lockAccount** - lock unlocked account
* **personal_updatePassphrase** - change passphrase of existing account

### Supported commands
* **create** - create new account and return address of account
* **insert** - insert command insert existing private key and store in keystore
* **update** - change passphrase of existing account

## ImportRawKey JSON-RPC Flow

``` sequence
    Network->Personal endpoint:personal_improtRawKey
    Personal endpoint->Manager:WalletsManager()\nKeyStoreType
    Manager->Personal endpoint:KeyStore\n(WalletManager)
    Personal endpoint->KeyStore:ImportECDSA()
    KeyStore->KeyStore:KeyEncrypt()
    KeyStore->AccountStore:add()
    KeyStore->Manager:WalletEvent
    Manager->Manager:Updater
    AccountStore->AccountStore:add
    Note right of AccountStore:KeyFile
```

## Insert Command Flow
``` sequence
    Command->Personal endpoint:personal_improtRawKey
    Personal endpoint->Manager:WalletsManager()\nKeyStoreType
    Manager->Personal endpoint:KeyStore\n(WalletManager)
    Personal endpoint->KeyStore:ImportECDSA()
    KeyStore->KeyStore:KeyEncrypt()
    KeyStore->AccountStore:add()
    KeyStore->Manager:WalletEvent
    Manager->Manager:Updater
    AccountStore->AccountStore:add
    Note right of AccountStore:KeyFile
```
    


## Private keys encryption 
Encryption of private keys is crucial for protecting blockchain users from unauthorized actions. We use the AES-128-CTR cryptographic algorithm to encrypt private keys, ensuring a high level of security. AES-128-CTR is a symmetric encryption algorithm, meaning the same key is used for both encryption and decryption.

To enhance security, we introduce an authentication string used to encrypt and decrypt private keys. From this authentication string, we derive an appropriate encryption key, which serves as the private key for AES-128-CTR. To access private keys, users must know their authentication password, which allows for the decryption of the private keys.

Once a private key is decrypted, it is temporarily stored in memory and deleted after each use to maintain security. Every user, when creating a new account or importing an existing key, determines their authentication string and must remember it.





