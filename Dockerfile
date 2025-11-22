FROM golang:1.22 AS builder

WORKDIR /src
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /bin/vpn-agent ./cmd/agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates

USER nobody:nogroup
WORKDIR /app
COPY --from=builder /bin/vpn-agent /usr/local/bin/vpn-agent

ENTRYPOINT ["/usr/local/bin/vpn-agent"]
