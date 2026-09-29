---
title: "Contributing"
weight: 7
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

`make all` is what CI runs. The soak test is separate and opt-in, because it is measured in
hours rather than seconds:

```sh
make soak            # 5 minutes of load with a node restarted every 30 seconds
make soak SOAK=4h    # the full run the numbers on the clustering page come from
```

## PR Guidelines

- Keep PRs small and focused on a single concern
- Include tests for new functionality
- Ensure `make all` passes before pushing
