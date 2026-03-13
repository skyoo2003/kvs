---
title: "CLI Usage"
weight: 2
---

The `kvs` binary exposes a small Cobra-based CLI for interacting with the key-value store.

## Basic Commands

```sh
$ kvs --help
$ kvs -v
$ kvs version
```

## Configuration

Use `--config` to load a specific Viper-compatible configuration file:

```sh
$ kvs --config config.yaml version
```

## Available Flags

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Show help for any command |
| `-v` | Show version information |
| `--config` | Path to config file |

## Further Reading

For Go API usage, see the [Overview](../overview/) documentation or the [Go package reference](https://pkg.go.dev/github.com/skyoo2003/kvs).
