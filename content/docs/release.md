---
title: "Release Process"
weight: 8
---

A release is one tag push. Everything else — binaries, checksums, the container image, the
Homebrew cask, the GitHub release and its notes — comes out of that. The workflow that does it is
[`.github/workflows/release.yaml`][release-workflow], driven by
[Changie](https://github.com/miniscruff/changie) for the notes and
[GoReleaser](https://goreleaser.com/) for the artifacts.

[release-workflow]: https://github.com/skyoo2003/kvs/blob/main/.github/workflows/release.yaml

## Cutting a release

Notes come first, because the workflow refuses to run without them.

```sh
changie new                 # one fragment per user-visible change, as the change is made
changie batch v1.2.3        # fold every fragment into changes/v1.2.3.md
changie merge               # prepend that file to CHANGELOG.md
```

`changie batch` empties `changes/unreleased/`, so **read what it produced before merging.** Two
things are worth looking for, both of which have happened here:

- A fragment with no `Issue` renders as `[#<no value>]` — a dead link in the notes. `changie batch
  v1.2.3 --dry-run` shows this without consuming anything.
- Changes that cancel out. Something added and removed again between two tags is not news to
  anyone upgrading, and a release note saying both is worse than saying neither. The test is
  whether someone on the previous release can observe the difference.

Commit the batched file and the changelog, merge to `main`, then:

```sh
git tag v1.2.3 && git push origin v1.2.3
```

### Before pushing the tag

| Check | Why |
|---|---|
| `changes/v1.2.3.md` exists on `main` | The workflow fails in two seconds without it, after the tag is already pushed |
| `make all` passes | Nothing downstream runs the tests |
| `go test -run TestPublicAPISurface .` passes | An unintended change to the exported Go surface is a broken promise — see [Compatibility](../compatibility/) |
| `goreleaser check` passes | Catches deprecated configuration before a floating GoReleaser version turns it into a failed release |
| `gh release list` has no stale draft | Release Drafter keeps a rolling draft; it does not collide with a real tag, but it lingers next to the release just cut |
| The tap still holds its formula | Deleting it before the cask exists leaves `brew install skyoo2003/tap/kvs` with nothing to resolve. The swap belongs after the release job, not before the tag — see below |

A local rehearsal that does everything except publish:

```sh
goreleaser release --snapshot --clean         # add --skip=docker without a docker daemon
```

The `goreleaser` job in `.github/workflows/cd.yml` runs the same command on every pull request
touching the build, so the release path is exercised before merge rather than for the first time on
a tag.

### Retiring the formula, once

kvs published a Homebrew formula until v1. GoReleaser writes `Casks/kvs.rb` from the first v1 tag
onward, and the tap has to be finished by hand — GoReleaser has no migration support, and neither
its cask options nor `conflicts:` move an install that already exists.

Do this **after** the release job has pushed the cask, in one commit, or the tap is briefly a tap
with no kvs in it:

```sh
gh api repos/skyoo2003/homebrew-tap/contents/tap_migrations.json -X PUT \
  -f message='Migrate kvs from formula to cask' \
  -f content="$(printf '{\n  "kvs": "skyoo2003/tap"\n}\n' | base64)"
# then delete Formula/kvs.rb and the copy at the tap root
```

The value is the tap, not a path to anything: Homebrew re-resolves the name there and finds the
cask. It only consults the file when the name resolves to nothing, which is why the formula has to
go in the same change — while it is there, it wins and the migration never fires.

## What comes out

| Artifact | Where |
|---|---|
| Archives — darwin, linux, windows × 386, amd64, arm64, armv7 (nine in total) | GitHub release |
| `CHECKSUMS` — sha256 for every archive | GitHub release |
| Container image | `ghcr.io/skyoo2003/kvs`, tagged `v1.2.3-alpine`, `v1.2-alpine`, `v1-alpine`, `latest-alpine` |
| Homebrew cask | `skyoo2003/homebrew-tap`, installed with `brew install skyoo2003/tap/kvs` |
| Release notes | The GitHub release body, taken from `changes/v1.2.3.md` |

Each archive carries the binary plus `LICENSE`, `README.md`, `CHANGELOG.md`, and
`CODE_OF_CONDUCT.md`. The binary reports the tag through `kvs version`.

**Every container tag but the first moves.** `latest-alpine`, `v1-alpine`, and `v1.2-alpine` point
at whatever was released most recently, which is why there are no pre-release tags: publishing
`v1.0.0-rc.1` would move `latest-alpine` to a release candidate. Rehearse with `--snapshot`
instead.

## Upgrading from v0.1.x

Measured by running v0.1.1 and this version side by side, not inferred from the diff.

**The Go library is unchanged.** `NewStore`, `Get`, `Put`, `Delete`, and `ErrKeyNotFound` keep the
signatures they had in v0.1.1 — the example in that release's README compiles and runs against v1
untouched. Everything else on the package is new. [Compatibility](../compatibility/) is what
promises to keep it that way.

**The command line is unchanged.** `kvs serve --http-addr … --grpc-addr …`, `--config`, `version`,
and `-v` all still mean what they meant. The flags added since are additions.

**One new listener appears.** v1 serves RESP on `127.0.0.1:6379` unless told otherwise, which
v0.1.1 did not have. It is loopback-only, so nothing new is reachable from off the machine, and a
port already in use is logged and skipped rather than being fatal:

```
kvs: listen resp: listen tcp 127.0.0.1:6379: bind: address already in use;
RESP is off (set --resp-addr to move it, or "none" to silence this)
```

Pass `--resp-addr none` to not have it at all.

**gRPC is unchanged.** `api/kvsv1/kvs.proto` has not been touched since v0.1.1.

**HTTP changes in one place.** A `405 Method Not Allowed` now carries the same JSON error body as
every other HTTP error instead of an empty response. Status codes, paths, and the bodies of `PUT`,
`GET`, `DELETE`, and `/healthz` — including their 404s — are byte-identical to v0.1.1.

**There is no data to migrate.** v0.1.1 had no `--data-dir`: the keyspace lived in memory and went
away with the process. Persistence arrived after it, so no released version of kvs ever wrote a
data directory. Starting v1 with `--data-dir` on an empty directory is the whole upgrade.

Point v1 at a directory written in a format it does not know and it refuses to start, saying so
rather than replaying bytes it does not understand:

```
open data dir /var/lib/kvs: /var/lib/kvs is format 999 and this build understands format 1.
kvs does not convert between them: run the version that wrote it, or move the directory aside
and load the data again.
```

**Homebrew installs a cask now, not a formula.** `brew install skyoo2003/tap/kvs` is the same
command; GoReleaser deprecated the formula path for pre-built binaries. Moving an install that
already exists is the tap's job, not this repository's — see the tap row in the pre-tag checks
above.

## Documentation site

The Hugo site at [skyoo2003.github.io/kvs](https://skyoo2003.github.io/kvs/) is published by
`.github/workflows/docs.yaml` on every push to `main` that touches `hugo.toml`, `content/`,
`layouts/`, `static/`, `README.md`, or `CONTRIBUTING.md`. Hugo builds into `public/`, which
`actions/upload-pages-artifact` and `actions/deploy-pages` publish. It needs the repository's Pages
setting to deploy from GitHub Actions.

## Further Reading

- [Compatibility](../compatibility/) — what v1 promises not to break
- [Contributing](../contributing/) — development commands and the pull request flow
