---
title: "Release Process"
weight: 4
---

The project uses [Changie](https://github.com/miniscruff/changie) for changelog management and [GoReleaser](https://goreleaser.com/) for automated releases.

## Commands

```sh
changie new                    # create a new changelog fragment
changie batch {version}        # batch fragments into a version file
changie merge                  # merge into CHANGELOG.md
```

## Application Release

1. Prepare changelog: `changie new` → `changie batch v0.x.0` → `changie merge`
2. Commit and push to `main`
3. Create and push a tag: `git tag v0.x.0 && git push origin v0.x.0`
4. GitHub Actions runs `.github/workflows/release.yaml`
5. GoReleaser builds binaries, Docker images, and Homebrew formula

### Artifacts

- **Binaries**: darwin/linux/windows (amd64, arm64) via GitHub Releases
- **Docker**: `ghcr.io/skyoo2003/kvs:{tag}-alpine`
- **Homebrew**: `skyoo2003/tap/kvs`

## Documentation Site

The Hugo documentation site is deployed automatically to [skyoo2003.github.io/kvs](https://skyoo2003.github.io/kvs/) on every push to `main` that touches `content/`, `layouts/`, `static/`, or documentation-related files.
