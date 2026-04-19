---
title: "CLI Usage"
weight: 2
---

The `kvs` binary exposes a Cobra-based CLI for interacting with the key-value store.

## Commands

### `serve`

Start the HTTP and gRPC servers.

```sh
$ kvs serve
$ kvs serve --http-addr :8080
$ kvs serve --grpc-addr :50051
$ kvs serve --http-addr :8080 --grpc-addr :50051
$ kvs --config config.yaml serve
```

| Flag | Description | Default |
|------|-------------|---------|
| `--http-addr` | HTTP listen address | `:3456` |
| `--grpc-addr` | gRPC listen address | `:3457` |
| `--config` | Path to Viper-compatible config file | — |

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
- [Go package reference](https://pkg.go.dev/github.com/skyoo2003/kvs)
