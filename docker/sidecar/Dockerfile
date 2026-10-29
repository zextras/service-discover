FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG JQ_VERSION=1.7.1
RUN apk add --no-cache curl && \
    curl -fsSL -o /out/jq --create-dirs \
        "https://github.com/jqlang/jq/releases/download/jq-${JQ_VERSION}/jq-linux-${TARGETARCH}" && \
    chmod +x /out/jq
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY pkg ./pkg
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/service-discover ./cmd/agent

FROM docker.io/envoyproxy/envoy:v1.32-latest
COPY --from=docker.io/hashicorp/consul:2.1 /bin/consul /usr/local/bin/consul
COPY --from=builder /out/jq /usr/local/bin/jq
COPY --from=builder /out/service-discover /usr/local/bin/service-discover

COPY docker/sidecar/entrypoint.sh /usr/local/bin/entrypoint.sh

ENV SERVICE_NAME=""
ENV SETUP_SCRIPT=""
ENV TOKEN_FILE=""

ENTRYPOINT ["entrypoint.sh"]
