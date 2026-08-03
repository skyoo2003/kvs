---
title: "Contributing"
weight: 6
---

Contributions are welcome! See [`CONTRIBUTING.md`](https://github.com/skyoo2003/kvs/blob/main/CONTRIBUTING.md) for the full guide.

## Quick Start

1. **Fork** the repository
2. **Clone** your fork and run `go mod download`
3. **Make changes** — run `make all` to verify lint, tests, and build pass
4. **Open a Pull Request** against `main`

## Development Commands

```sh
make all       # lint + test + build
make test      # run tests
make lint      # run golangci-lint
make build     # build binary
```

## PR Guidelines

- Keep PRs small and focused on a single concern
- Include tests for new functionality
- Ensure `make all` passes before pushing
