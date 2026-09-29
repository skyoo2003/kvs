# Contributing to KVS

Thanks for your interest in contributing!

## Prerequisites

- Go 1.24+
- Make
- golangci-lint v2 (installed automatically by CI, or via [golangci-lint](https://golangci-lint.run/usage/install/)) — `.golangci.yaml` is v2-only, so a v1 binary will refuse to load it

## Project Structure

```
.
├── cmd/kvs/            # Thin CLI entrypoint
├── pkg/kvs/            # Public Store library and append log
├── internal/app/kvs/   # Cobra/Viper CLI application
├── internal/server/    # HTTP, gRPC, and RESP server implementations
├── internal/cluster/   # Raft membership, kept out of the library API
├── api/kvsv1/          # Generated protobuf/gRPC code
├── internal/resp/      # Private RESP2 wire protocol codec
├── website/            # Hugo documentation site
└── changes/            # Changelog fragments (Changie)
```

### Project Layout Guidelines

Follow [golang-standards/project-layout](https://github.com/golang-standards/project-layout)
where it fits this repository. Create directories only when they have a clear purpose; do not
add empty placeholders or a top-level `src/` directory.

- `cmd/<binary>/` contains a minimal `main` package that wires arguments, standard streams, and
  build metadata into an internal application package.
- `internal/app/<binary>/` owns CLI composition. `internal/` owns code that external Go modules
  must not import, including server, cluster, persistence, and protocol implementation.
- `pkg/<library>/` contains libraries intentionally supported for external import. Public KVS
  code belongs in `pkg/kvs`; do not add non-public packages under `pkg/`.
- `api/` contains source protocol contracts and generated API bindings.
- `build/package/` contains Docker and release-packaging configuration. Keep tool-required
  workflow files and repository-root configuration at their required locations.
- `website/` contains the Hugo project; do not commit its generated `public/` and `resources/`
  directories.
- Keep unit tests next to their packages and package-specific Go test fixtures in that package's
  `testdata/` directory.

When relocating code, preserve public import paths and behavior unless a change explicitly
authorizes a breaking API migration. Update imports, build/release automation, ownership rules,
and documentation in the same change.

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
make notice    # verify third-party notice inventory
make clean     # remove dist/
```

`make all` is what CI runs. The long one is separate and opt-in:

```sh
make soak            # 5 minutes of load with a node restarted every 30 seconds
make soak SOAK=4h    # the full run the numbers in the docs come from
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

By submitting a contribution, you confirm that you have the right to submit it
and license it under the [Apache License 2.0](LICENSE). Do not include copied
or generated third-party material unless its license and required notices are
identified for inclusion in the release notice bundle.

- Keep PRs small and focused on a single concern
- Include tests for new functionality
- Add a changelog fragment for user-visible changes: `changie new`, one sentence, two at most.
  `.changie.yaml` caps a body at 280 characters, which is about where a second sentence ends; a
  fragment that needs more is carrying documentation, which belongs on a page the notes can link to
  (see [Release Process](https://skyoo2003.github.io/kvs/docs/release/))
- Ensure `make all` passes (lint + test + build)
- Update documentation if behavior changes

## Reporting Bugs

Please open an issue with:
- Go version and OS
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs or error messages
