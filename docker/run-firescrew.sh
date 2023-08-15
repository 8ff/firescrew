#!/bin/sh

fail() {
  echo "ERROR: $1"
  exit 1
}

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
RTSP_SERVER_BINARY_PATH="/bins/rtspServer.linux.${BINARY_ARCH}"

if [ ! -f $BINARY_PATH ]; then
  echo "Binary not found for Architecture: $ARCH"
  exit 1
fi

# Check if the first and only parameter is "demo"
if [ "$#" -eq 1 ] && [ "$1" = "demo" ]; then
  echo "******* Running in DEMO mode *******"
  rm -rf /demo
  mkdir /demo && cd /demo || fail "Failed to create demo directory"
  git clone https://github.com/8ff/firescrew 2>/dev/null || fail "Failed to clone demo repository"
  cd firescrew/demoStream || fail "Failed to change directory to demoStream"



  # Start the RTSP servers and save their PIDs
  exec $RTSP_SERVER_BINARY_PATH :8553 >/pid1.log 2>&1 || fail "Failed to start RTSP server" &
  PID1=$!
  exec $RTSP_SERVER_BINARY_PATH :8554 >/pid2.log 2>&1 || fail "Failed to start RTSP server" &
  PID2=$!

  echo "[+] Downloading sample video..."
  curl -o sample.mp4 -L https://7ff.org/sample.mp4 1>/dev/null 2>&1|| fail "Failed to download sample video"
  nohup ffmpeg -stream_loop -1 -re -i sample.mp4 \
  -c:v libx264 -preset veryfast -tune zerolatency -g 5 -r 25 -s 640x360 -b:v 1500k -rtsp_transport tcp -f rtsp rtsp://localhost:8553/lo \
  -c:v libx264 -preset veryfast -tune zerolatency -g 5 -r 25 -s 1920x1080 -b:v 5000k -rtsp_transport tcp -f rtsp rtsp://localhost:8554/hi >/pid3.log 2>&1|| fail "Failed to start ffmpeg" &
  PID3=$!


  exec nohup $BINARY_PATH ./demoConfig.json >/pid4.log 2>&1|| fail "Failed to start firescrew" &
  PID4=$!

  exec $BINARY_PATH -s rec/hi :8080 1>/pid5.log 2>&1 || fail "Failed to serve WebUI" &
  PID5=$!

  echo "[+] READY"

  # Block here
  while true; do sleep 1; done

# Function to kill the RTSP servers when the script exits
cleanup() {
  kill -9 $PID1 $PID2 $PID3 $PID4 $PID5
}

# Trap the EXIT signal and call the cleanup function
trap cleanup EXIT


else
  exec $BINARY_PATH "$@"
fi
