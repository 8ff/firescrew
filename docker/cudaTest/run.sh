# On host
#curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey > /etc/apt/keyrings/nvidia-docker.key 
#curl -s -L https://nvidia.github.io/nvidia-docker/debian11/nvidia-docker.list > /etc/apt/sources.list.d/nvidia-docker.list 
#sed -i -e "s/^deb/deb \[signed-by=\/etc\/apt\/keyrings\/nvidia-docker.key\]/g" /etc/apt/sources.list.d/nvidia-docker.list
#apt update 
#apt -y install nvidia-container-toolkit 
#systemctl restart docker 

docker build --no-cache -t cudafirescrew -f docker/cudaTest/Dockerfile . && docker run --gpus all --rm -it cudafirescrew bash