---
title: "Overview"
weight: 1
---

KVS is a key-value store written in Go. It provides an in-memory store that can be used as a library, via its CLI, or as a server with HTTP and gRPC interfaces.

## Features

- **In-memory key-value store** — thread-safe `Store` with `Get`, `Put`, `Delete`
- **HTTP server** — JSON REST API at `/v1/keys/{key}` with health check at `/healthz`
- **gRPC server** — protobuf-based KV service with gRPC health checking
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
docker run ghcr.io/skyoo2003/kvs:latest-alpine
```

### Homebrew

```sh
brew install skyoo2003/tap/kvs
```

## Quick Start

### CLI

```sh
kvs serve                     # start HTTP (:3456) and gRPC (:3457) servers
kvs serve --http-addr :8080   # custom HTTP address
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
