---
title: "Overview"
weight: 1
---

KVS is a key-value store written in Go. It provides an in-memory store that can be used as a library, via its CLI, or as a server with HTTP, gRPC, and Redis/Valkey compatible interfaces.

## Features

- **In-memory key-value store** — thread-safe `Store` with `Get`, `Put`, `Delete`
- **HTTP server** — JSON REST API at `/v1/keys/{key}` with health check at `/healthz`
- **gRPC server** — protobuf-based KV service with gRPC health checking
- **Redis/Valkey API** — RESP2 server on `:6379`, so `redis-cli` and `go-redis` connect directly
- **Cobra/Viper CLI** — `serve` command with configurable listen addresses
- **Library** — import `github.com/skyoo2003/kvs` directly in Go programs
- **Docker** — container images published to `ghcr.io/skyoo2003/kvs`

## Installation

### From source

```sh
go install github.com/skyoo2003/kvs/cmd/kvs@latest
```

### Docker

```sh
docker run -p 3456:3456 -p 3457:3457 -p 6379:6379 \
  -e KVS_RESP_ADDR=:6379 ghcr.io/skyoo2003/kvs:latest-alpine
```

## Quick Start

### CLI

```sh
kvs serve                     # HTTP (:3456), gRPC (:3457), RESP (127.0.0.1:6379)
kvs serve --http-addr :8080   # custom HTTP address
kvs serve --resp-addr :6379   # expose RESP beyond loopback
kvs version                   # print version
```

### Library

Import `github.com/skyoo2003/kvs` to use KVS as a library:

```go
package main

import (
	"fmt"
	"github.com/skyoo2003/kvs"
)

func main() {
	store := kvs.NewStore()
	_ = store.Put("language", "go")

	value, _ := store.Get("language")
	fmt.Println(value)
}
```

## License

MIT License. See [LICENSE](https://github.com/skyoo2003/kvs/blob/main/LICENSE) for details.
