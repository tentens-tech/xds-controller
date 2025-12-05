# Build the xds binary
FROM golang:1.25 AS builder
ARG TARGETOS
ARG ARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY apis/ apis/
COPY cmd/ cmd/
COPY controllers/ controllers/
COPY pkg/ pkg/

# Build with version information
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${ARCH} go build -a \
    -ldflags "-X github.com/tentens-tech/xds-controller/pkg/version.Version=${VERSION} \
              -X github.com/tentens-tech/xds-controller/pkg/version.GitCommit=${GIT_COMMIT} \
              -X github.com/tentens-tech/xds-controller/pkg/version.BuildDate=${BUILD_DATE}" \
    -o xds /workspace/cmd/xds/main.go

FROM alpine:3.23

RUN apk --no-cache add ca-certificates libcrypto3 libssl3 \
    && mkdir -m 0777 /certs

WORKDIR /
COPY --from=builder /workspace/xds .
USER 65532:65532

ENTRYPOINT ["/xds"]
