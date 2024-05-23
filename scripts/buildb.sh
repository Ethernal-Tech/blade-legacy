#!/bin/bash

make build
mv ./blade ~/go/bin
./scripts/delete_folders.sh