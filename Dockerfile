FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /patrol-server ./cmd/server

FROM golang:1.26-bookworm AS tester
WORKDIR /src
COPY --from=builder /src/ .
RUN go test -timeout=120s -count=1 ./...

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /patrol-server /patrol-server
EXPOSE 48235
ENTRYPOINT ["/patrol-server"]
