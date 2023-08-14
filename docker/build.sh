#!/bin/bash
docker buildx build --no-cache --push --platform linux/arm64,linux/amd64 -t 8fforg/firescrew:latest -f docker/Dockerfile .
