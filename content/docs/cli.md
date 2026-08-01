---
title: "CLI Usage"
weight: 2
---

The `kvs` binary exposes a Cobra-based CLI for interacting with the key-value store.

## Commands

### `serve`

Start the HTTP, gRPC, and RESP servers.

```sh
$ kvs serve
$ kvs serve --http-addr :8080
$ kvs serve --grpc-addr :50051
$ kvs serve --resp-addr :6379
$ kvs serve --resp-addr none
$ kvs serve --data-dir /var/lib/kvs                     # keep the keyspace across restarts
$ kvs serve --data-dir /var/lib/kvs \
            --raft-addr 10.0.0.1:7901                   # run as a cluster node
$ kvs --config config.yaml serve
```

| Flag | Description | Default |
|------|-------------|---------|
| `--http-addr` | HTTP listen address | `:3456` |
| `--grpc-addr` | gRPC listen address | `:3457` |
| `--resp-addr` | Redis/Valkey (RESP) listen address, `none` to disable | `127.0.0.1:6379` |
| `--data-dir` | Directory to keep the keyspace in; empty keeps it in memory only | — (in memory) |
| `--raft-addr` | Address the other cluster nodes reach this one on; enables clustering | — (no cluster) |
| `--join` | Redis address of a node already in the cluster; omit on the first node | — (starts a cluster) |
| `--node-id` | Stable identity, and the address clients are redirected to | `--resp-addr` |
| `--config` | Path to Viper-compatible config file | — |

If the default RESP port is already taken, kvs warns and starts without it. An address you
name yourself has to bind, or `serve` fails.

`--raft-addr` has to name an address the other nodes can actually reach it on, so a bare
`:7901` is refused — `local bind address is not advertisable` — where `127.0.0.1:7901` or
`10.0.0.1:7901` is accepted. It also needs `--data-dir`, because the Raft log has to live
somewhere, and a `--node-id`, which it borrows from `--resp-addr` unless you say otherwise, so
`--resp-addr none` means naming one yourself. All three are refused at startup rather than
surfacing later. What a cluster does and does not promise is on the
[Durability and Clustering](../clustering/) page.

### Configuration without flags

Every flag has a config file and environment equivalent. The RESP password has **only**
those: a credential passed as an argument is visible to anything that can list processes.

| Setting | Config key | Environment |
|---------|-----------|-------------|
| HTTP address | `http_addr` | `KVS_HTTP_ADDR` |
| gRPC address | `grpc_addr` | `KVS_GRPC_ADDR` |
| RESP address | `resp_addr` | `KVS_RESP_ADDR` |
| RESP password | `resp_password` | `KVS_RESP_PASSWORD` |
| Data directory | `data_dir` | `KVS_DATA_DIR` |
| Raft address | `raft_addr` | `KVS_RAFT_ADDR` |
| Cluster to join | `join` | `KVS_JOIN` |
| Node identity | `node_id` | `KVS_NODE_ID` |

### `version`

Print the CLI version.

```sh
$ kvs version
$ kvs -v
```

## Available Flags

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Show help for any command |
| `-v` | Show version information |
| `--config` | Path to config file |

## Further Reading

- [Overview](../overview/) — library usage and installation
- [HTTP API](../http-api/) — REST endpoint details
- [Redis API](../redis-api/) — supported RESP commands and behaviour notes
- [Durability and Clustering](../clustering/) — what `--data-dir` and `--raft-addr` promise
- [Go package reference](https://pkg.go.dev/github.com/skyoo2003/kvs)
