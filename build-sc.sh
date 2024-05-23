#!/bin/bash

BRANCH=main # optimization-chain-registration

cd ./apex-bridge-smartcontracts
git checkout main
git fetch origin
git pull origin
if [ "$BRANCH" != "main" ]; then
    PRINT "SWITCHING TO ${BRANCH}"
    git branch -D ${BRANCH}
    git switch ${BRANCH}
    git pull origin # this is not important but lets have it here
fi
npm i && npx hardhat compile
cd ..
go run consensus/polybft/contractsapi/apex-artifacts-gen/main.go
go run consensus/polybft/contractsapi/bindings-gen/main.go
./buildb.sh