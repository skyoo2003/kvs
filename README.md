# KVS

A key-value store as a distributed server or a Go module.

[![CI](https://github.com/skyoo2003/kvs/actions/workflows/ci.yaml/badge.svg)](https://github.com/skyoo2003/kvs/actions/workflows/ci.yaml) [![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/kvs.svg)](https://pkg.go.dev/github.com/skyoo2003/kvs) [![Go Report Card](https://goreportcard.com/badge/github.com/skyoo2003/kvs)](https://goreportcard.com/report/github.com/skyoo2003/kvs)

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
docker run -p 3456:3456 -p 3457:3457 ghcr.io/skyoo2003/kvs:latest-alpine
```

## Usage

### CLI

```sh
$ kvs serve                          # start HTTP (:3456) and gRPC (:3457) servers
$ kvs serve --http-addr :8080        # custom HTTP address
$ kvs serve --grpc-addr :50051       # custom gRPC address
$ kvs --config config.yaml serve     # load addresses from Viper config
$ kvs version                        # print version
```

### Library

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

### HTTP API

```sh
# Put
curl -X PUT http://localhost:3456/v1/keys/mykey -d '{"value": "hello"}'

# Get
curl http://localhost:3456/v1/keys/mykey

# Delete
curl -X DELETE http://localhost:3456/v1/keys/mykey
```

## Documentation

Full documentation is available at [skyoo2003.github.io/kvs](https://skyoo2003.github.io/kvs).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the project contribution process.

## [License](LICENSE)

The MIT License

Copyright (c) 2020-2026 Sung-Kyu Yoo
