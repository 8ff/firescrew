#!/bin/bash
apt-get update && apt-get install -y git rsync
curl -L -o /go1.21.0.linux-amd64.tar.gz https://go.dev/dl/go1.21.0.linux-amd64.tar.gz && tar -xzvf /go1.21.0.linux-amd64.tar.gz -C /usr/local
export PATH=$PATH:/usr/local/go/bin
export LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/local/lib/python3.10/dist-packages/torch/lib
git clone https://github.com/8ff/firescrew
cd firescrew/pkg/objectPredict
./getLibs.sh && ./compressLibs.sh