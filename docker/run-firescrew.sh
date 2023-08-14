#!/bin/sh

# Determine the architecture
ARCH=$(uname -m)

# Map the architecture to the binary name
case $ARCH in
  x86_64) BINARY_ARCH=amd64 ;;
  aarch64) BINARY_ARCH=arm64 ;;
  # Add other architectures if needed
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

BINARY_PATH="/bins/firescrew.linux.${BINARY_ARCH}"

if [ ! -f $BINARY_PATH ]; then
  echo "Binary not found for Architecture: $ARCH"
  exit 1
fi

# Run the binary
exec $BINARY_PATH "$@"
