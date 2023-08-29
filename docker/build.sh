#!/bin/bash
curl -L https://7ff.org/lib.tgz -o pkg/objectPredict/lib.tgz
docker buildx build --no-cache --push --platform linux/arm64,linux/amd64 -t 8fforg/firescrew:latest -f docker/Dockerfile .
