#!/bin/bash

go build -o rtspServer/rtspServer rtspServer/rtspServer.go

# Start the RTSP servers and save their PIDs
rtspServer/rtspServer :8553 &
PID1=$!
rtspServer/rtspServer :8554 &
PID2=$!

# Function to kill the RTSP servers when the script exits
cleanup() {
  kill $PID1 $PID2
}

# Trap the EXIT signal and call the cleanup function
trap cleanup EXIT

ffmpeg -stream_loop -1 -re -i sample.mp4 \
  -c:v libx264 -preset veryfast -tune zerolatency -g 5 -r 25 -s 640x360 -b:v 1500k -rtsp_transport tcp -f rtsp rtsp://localhost:8553/lo \
  -c:v libx264 -preset veryfast -tune zerolatency -g 5 -r 25 -s 1920x1080 -b:v 5000k -rtsp_transport tcp -f rtsp rtsp://localhost:8554/hi