# KVS

A key-value store you can run as a server or import as a Go module.

[![CI](https://github.com/skyoo2003/kvs/actions/workflows/ci.yaml/badge.svg)](https://github.com/skyoo2003/kvs/actions/workflows/ci.yaml) [![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/kvs.svg)](https://pkg.go.dev/github.com/skyoo2003/kvs) [![Go Report Card](https://goreportcard.com/badge/github.com/skyoo2003/kvs)](https://goreportcard.com/report/github.com/skyoo2003/kvs)

## Features

- **In-memory key-value store** — thread-safe `Store` with `Get`, `Put`, `Delete`
- **HTTP server** — JSON REST API at `/v1/keys/{key}` with health check at `/healthz`
- **gRPC server** — protobuf-based KV service with gRPC health checking
- **Redis/Valkey API** — RESP2 server on `127.0.0.1:6379`, so `redis-cli` and `go-redis` connect directly
- **Durability** — `--data-dir` appends every change to a log and replays it at startup, so the keyspace survives a restart
- **Clustering** — `--raft-addr` joins a Raft cluster, so losing the leader costs an election rather than a person
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

## Usage

### CLI

```sh
$ kvs serve                          # start HTTP (:3456) and gRPC (:3457) servers
$ kvs serve --http-addr :8080        # custom HTTP address
$ kvs serve --grpc-addr :50051       # custom gRPC address
$ kvs serve --resp-addr :6379        # expose RESP beyond loopback
$ kvs serve --resp-addr none         # disable the RESP listener
$ kvs serve --data-dir /var/lib/kvs  # keep the keyspace across restarts
$ kvs serve --raft-addr :7901        # run as a cluster node
$ kvs serve --join host:6379         # join an existing cluster
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

### Redis/Valkey API

All three protocols share one keyspace, so any client can read what another wrote.

```sh
$ redis-cli -p 6379 set mykey hello
OK
$ redis-cli -p 6379 get mykey
"hello"
$ curl http://localhost:3456/v1/keys/mykey
{"key":"mykey","value":"hello"}
```

The RESP listener binds `127.0.0.1:6379` by default and takes a password from
`KVS_RESP_PASSWORD`. See the [Redis API docs](https://skyoo2003.github.io/kvs/docs/redis-api/)
for the supported command list.

### Durability

Without `--data-dir` everything lives in memory and a restart starts empty, which is the
default. Point it at a directory and every change is appended to a log there and replayed at
startup, across all three protocols.

```sh
$ kvs serve --data-dir /var/lib/kvs
$ redis-cli -p 6379 set greeting hello
OK
# restart kvs
$ redis-cli -p 6379 get greeting
"hello"
```

A write that has returned is on disk: the log is synced before the command answers, so an
acknowledged write survives a crash and not only a clean shutdown. On one node that is durability
and not availability — a lost disk is lost data and a stopped process is a stopped service, which
is what the cluster below is for. The rest of what the log does and does not promise is in the
[durability and clustering docs](https://skyoo2003.github.io/kvs/docs/clustering/).

### Clustering

`--raft-addr` puts a node in a Raft cluster. Writes go to whoever is leader and are only
acknowledged once a majority has them; if the leader dies, the rest elect a new one and writes
carry on with nobody doing anything.

Run three. The first starts the cluster, the others join it through any existing node's Redis
port.

```sh
# first node — starts the cluster
$ kvs serve --data-dir /var/lib/kvs1 --raft-addr 127.0.0.1:7901 --resp-addr 127.0.0.1:6381

# the others — join it
$ kvs serve --data-dir /var/lib/kvs2 --raft-addr 127.0.0.1:7902 --resp-addr 127.0.0.1:6382 \
            --join 127.0.0.1:6381
$ kvs serve --data-dir /var/lib/kvs3 --raft-addr 127.0.0.1:7903 --resp-addr 127.0.0.1:6383 \
            --join 127.0.0.1:6381

$ redis-cli -p 6381 set greeting hello
OK
$ redis-cli -p 6382 get greeting            # reads work anywhere
"hello"
$ redis-cli -p 6382 set greeting bye        # writes go to the leader
(error) MOVED 0 127.0.0.1:6381
```

Stop the leader and the cluster elects another, with nobody doing anything. Everything written
before the failover is still there.

What that does and does not promise:

- **Three nodes, not two.** A majority of two is two, so a two-node cluster stops taking writes
  the moment either one goes down — worse than a single node. Run an odd number, three or more.
- A write is acknowledged only once a majority has it on disk, so an acknowledged write survives
  losing a minority of the cluster. That is strong consistency, at one consensus round per write.
- **Writes stop during an election.** Measured on three nodes on one machine, they came back
  between 1.3 and 3.0 seconds after the leader was stopped, over eight runs; expect longer over a
  real network. There is no way around the gap — the cluster is choosing who may accept writes.
- Reads are answered by any node, from its own copy, and may be behind the leader.
- A write sent to a node that is not the leader is refused and told where to go: `MOVED` over
  RESP, `409` over HTTP, `FAILED_PRECONDITION` over gRPC.
- **Publish/subscribe does not cross nodes**, and `/healthz` says only that the process is
  listening — a node that has lost the majority still answers `200`.
- `--data-dir` is required in a cluster, and so is a `--node-id` if there is no `--resp-addr` to
  borrow one from.
- No sharding. Every node holds the whole keyspace, and writes are serialized through the leader.
  This is built for staying up, not for throughput.

The [durability and clustering docs](https://skyoo2003.github.io/kvs/docs/clustering/) go through
each of these, and what to do about them.

## Compatibility

What `v1` promises not to break, and what it deliberately leaves out, is on the
[compatibility page](https://skyoo2003.github.io/kvs/docs/compatibility/). Read it before
pinning kvs, and before opening a port: kvs has no authorization and no TLS, only the RESP
listener can be given a password, and HTTP and gRPC listen on every interface by default.

## Documentation

Full documentation is available at [skyoo2003.github.io/kvs](https://skyoo2003.github.io/kvs).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the project contribution process.

## [License](LICENSE)

The MIT License

Copyright (c) 2020-2026 Sung-Kyu Yoo
