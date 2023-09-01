FROM nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04
RUN apt-get update && apt-get -y install build-essential curl
RUN curl -L -o /go1.21.0.linux-amd64.tar.gz https://go.dev/dl/go1.21.0.linux-amd64.tar.gz && tar -xzvf /go1.21.0.linux-amd64.tar.gz -C /usr/local
ENV PATH=$PATH:/usr/local/go/bin
WORKDIR /build
COPY . .
RUN go mod tidy
