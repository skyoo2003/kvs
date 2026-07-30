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
$ kvs --config config.yaml serve
```

| Flag | Description | Default |
|------|-------------|---------|
| `--http-addr` | HTTP listen address | `:3456` |
| `--grpc-addr` | gRPC listen address | `:3457` |
| `--resp-addr` | Redis/Valkey (RESP) listen address, `none` to disable | `127.0.0.1:6379` |
| `--config` | Path to Viper-compatible config file | — |

### Configuration without flags

Every flag has a config file and environment equivalent. The RESP password has **only**
those: a credential passed as an argument is visible to anything that can list processes.

| Setting | Config key | Environment |
|---------|-----------|-------------|
| HTTP address | `http_addr` | `KVS_HTTP_ADDR` |
| gRPC address | `grpc_addr` | `KVS_GRPC_ADDR` |
| RESP address | `resp_addr` | `KVS_RESP_ADDR` |
| RESP password | `resp_password` | `KVS_RESP_PASSWORD` |

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
- [Go package reference](https://pkg.go.dev/github.com/skyoo2003/kvs)
