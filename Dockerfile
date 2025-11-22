FROM golang:1.22 AS builder

WORKDIR /src
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /bin/node-agent ./cmd/agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates

USER nobody:nogroup
WORKDIR /app
COPY --from=builder /bin/node-agent /usr/local/bin/node-agent

ENTRYPOINT ["/usr/local/bin/node-agent"]
