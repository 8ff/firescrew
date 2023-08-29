#!/bin/bash
function get_linux_amd64_cpu() {
    verString="linux-x64-1.15.1"
    dstDir="linux_amd64_cpu"
    rm -rf ${dstDir}; mkdir -p ${dstDir} 2>/dev/null
    curl -o onnxruntime-${verString}.tgz -L https://github.com/microsoft/onnxruntime/releases/download/v1.15.1/onnxruntime-${verString}.tgz
    tar -xzf onnxruntime-${verString}.tgz --strip-components=2 -C ${dstDir} onnxruntime-${verString}/lib/ && rm onnxruntime-${verString}.tgz 2>/dev/null
}

function get_linux_amd64_gpu() {
    verString="linux-x64-gpu-1.15.1"
    dstDir="linux_amd64_gpu"
    rm -rf ${dstDir}; mkdir -p ${dstDir} 2>/dev/null
    curl -o onnxruntime-${verString}.tgz -L https://github.com/microsoft/onnxruntime/releases/download/v1.15.1/onnxruntime-${verString}.tgz
    tar -xzf onnxruntime-${verString}.tgz --strip-components=2 -C ${dstDir} onnxruntime-${verString}/lib/ && rm onnxruntime-${verString}.tgz 2>/dev/null
}

function get_linux_arm64_cpu() {
    verString="linux-aarch64-1.15.1"
    dstDir="linux_arm64_cpu"
    rm -rf ${dstDir}; mkdir -p ${dstDir} 2>/dev/null
    curl -o onnxruntime-${verString}.tgz -L https://github.com/microsoft/onnxruntime/releases/download/v1.15.1/onnxruntime-${verString}.tgz
    tar -xzf onnxruntime-${verString}.tgz --strip-components=2 -C ${dstDir} onnxruntime-${verString}/lib/ && rm onnxruntime-${verString}.tgz 2>/dev/null
}


function get_osx_arm64() {
    verString="osx-arm64-1.15.1"
    dstDir="osx_arm64"
    rm -rf ${dstDir}; mkdir -p ${dstDir} 2>/dev/null
    curl -o onnxruntime-${verString}.tgz -L https://github.com/microsoft/onnxruntime/releases/download/v1.15.1/onnxruntime-${verString}.tgz
    tar -xzf onnxruntime-${verString}.tgz --strip-components=3 -C ${dstDir} ./onnxruntime-${verString}/lib/ && rm ./onnxruntime-${verString}.tgz 2>/dev/null
}


# If lib folder exists and is not empty, then give error
if [ -d "lib" ] && [ "$(ls -A lib)" ]; then
    echo "ERROR: lib folder exists and is not empty"
    exit 1
fi

# If lib folder doesnt exist, create it
if [ ! -d "lib" ]; then
    mkdir lib
fi

# Change directory to lib
cd lib
get_linux_amd64_cpu
get_linux_amd64_gpu
get_linux_arm64_cpu
get_osx_arm64