FROM ultralytics/ultralytics:latest AS ultralytics-stage
RUN pip3 install tflite-runtime numpy Pillow

FROM golang:latest AS firescrew-build-stage
WORKDIR /build
COPY . .
RUN ls -la; go mod download
RUN mkdir /bins
RUN GOOS=linux GOARCH=amd64 go build -ldflags "-X 'main.Version=$(date +'%Y-%m-%d_%H:%M:%S')'" -o /bins/firescrew.linux.amd64
RUN GOOS=linux GOARCH=arm64 go build -ldflags "-X 'main.Version=$(date +'%Y-%m-%d_%H:%M:%S')'" -o /bins/firescrew.linux.arm64

FROM debian:latest
# REMOVE THIS
COPY --from=firescrew-build-stage /build /build
COPY --from=firescrew-build-stage /bins /bins
COPY --from=ultralytics-stage /usr/src/ultralytics /usr/src/ultralytics
COPY --from=ultralytics-stage /opt/conda/lib/python3.10/site-packages /opt/conda/lib/python3.10/site-packages
RUN apt-get update && apt-get install -y curl ffmpeg python3 python3-pip

COPY --from=firescrew-build-stage /build/docker/run-firescrew.sh /run-firescrew.sh
RUN chmod +x /run-firescrew.sh
ENTRYPOINT ["/run-firescrew.sh"]
CMD []