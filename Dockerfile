# Build binary
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.21 as builder
ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH
ENV BINARYNAME=storage-metrics-exporter
WORKDIR /src
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-X main.Commit=$(git rev-parse HEAD)" -a -o ${BINARYNAME:-prometheus-metrics-exporter} main.go

# Build distroless container from binary
FROM --platform=${BUILDPLATFORM:-linux/amd64} gcr.io/distroless/static:nonroot
LABEL org.opencontainers.image.source="https://github.com/Cloud-for-You/kubernetes-storage-metrics"
WORKDIR /
COPY --from=builder /src/${BINARYNAME:-prometheus-metrics-exporter} .
ENTRYPOINT ["/${BINARYNAME:-prometheus-metrics-exporter}"]