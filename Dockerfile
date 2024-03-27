FROM docker.intuit.com/docker-rmt/alpine:latest

ARG TARGETOS
ARG TARGETARCH
ARG SOURCE_BINARY="dist/remote-tsdb-clickhouse-linux-amd64"

WORKDIR /
COPY ${SOURCE_BINARY} remote-tsdb-clickhouse

ENTRYPOINT ["/remote-tsdb-clickhouse"]
CMD ["-h"]