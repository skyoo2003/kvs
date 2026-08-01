# Contributing to KVS

Thanks for your interest in contributing!

## Prerequisites

- Go 1.24+
- Make
- golangci-lint v2 (installed automatically by CI, or via [golangci-lint](https://golangci-lint.run/usage/install/)) — `.golangci.yaml` is v2-only, so a v1 binary will refuse to load it

## Project Structure

```
.
├── kvs.go              # Store (Get, Put, Delete), in memory unless Open gives it a log
├── log.go              # Append log the keyspace survives a restart through
├── replication.go      # The store surface a cluster drives it through
├── cmd/kvs/            # CLI entrypoint (Cobra)
├── internal/server/    # HTTP, gRPC, and RESP server implementations
├── internal/cluster/   # Raft membership, kept out of the library API
├── api/kvsv1/          # Generated protobuf/gRPC code
├── pkg/resp/           # RESP2 wire protocol codec
├── content/            # Hugo documentation site
└── changes/            # Changelog fragments (Changie)
```

## Getting Started

1. **Fork** the repository
2. **Clone** your fork locally
3. **Install dependencies**: `go mod download`
4. **Create a branch**: `git checkout -b my-feature`
5. **Make changes** and commit with clear messages
6. **Push** to your fork: `git push origin my-feature`
7. **Open a Pull Request** against `main`

## Development

```sh
make all       # lint + test + build
make lint      # run golangci-lint
make test      # run tests
make build     # build binary to dist/
make clean     # remove dist/
```

### Pre-commit Hooks (optional)

```sh
make setup     # installs pre-commit hooks
```

## Code Style

- Go files use **tabs** for indentation (see `.editorconfig`)
- YAML files use **2 spaces**
- Run `make all` before pushing — CI enforces the same checks

## PR Guidelines

- Keep PRs small and focused on a single concern
- Include tests for new functionality
- Ensure `make all` passes (lint + test + build)
- Update documentation if behavior changes

## Reporting Bugs

Please open an issue with:
- Go version and OS
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs or error messages
