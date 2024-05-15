cd ./apex-bridge-smartcontracts
git checkout main
# git pull origin
# git branch -D feat/tries
# git fetch origin
# git switch feat/tries
git pull origin
npm i && npx hardhat compile
cd ..
go run consensus/polybft/contractsapi/apex-artifacts-gen/main.go
go run consensus/polybft/contractsapi/bindings-gen/main.go
./buildb.sh