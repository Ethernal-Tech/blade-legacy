cd ./apex-bridge-smartcontracts
git checkout main
git pull origin
git branch -d feat/tries
git fetch origin
git switch feat/tries
npm i && npx hardhat compile
cd ..
go run consensus/polybft/contractsapi/apex-artifacts-gen/main.go
go run consensus/polybft/contractsapi/bindings-gen/main.go
./buildb.sh