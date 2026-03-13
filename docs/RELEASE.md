# Release

This repository has two release paths:

- tagged application releases through `.github/workflows/release.yaml`
- static documentation site releases through `.github/workflows/docs.yaml`

## Commands

```sh
$ changie new
$ changie batch {version}
$ changie merge
```

## Project Release

Project releases are published from `.github/workflows/release.yaml` when a tag matching `v*` is pushed.

### Preparation

1. Create changelog entries with `changie new`
2. Batch the release notes with `changie batch {version}`
3. Merge the changelog with `changie merge`
4. Commit the release notes and create the release tag

### Publish Flow

1. Push a tag such as `v1.2.3`
2. GitHub Actions runs `.github/workflows/release.yaml`
3. GoReleaser builds the `kvs` binaries and archives defined in `.goreleaser.yml`
4. The workflow publishes the GitHub release artifacts and related package outputs

### Verification

After pushing the tag, confirm that the `Release` workflow succeeds and that the expected release artifacts are attached to the GitHub release.

## Documentation Site Release

The static documentation site is built with Hugo and published through GitHub Pages Actions.

### Trigger

The docs workflow runs on pushes to `main` when one of these paths changes:

- `hugo.toml`
- `content/**`
- `layouts/**`
- `static/**`
- `README.md`
- `CONTRIBUTING.md`
- `docs/RELEASE.md`
- `.github/workflows/docs.yaml`

### Publish Flow

1. GitHub Actions runs `.github/workflows/docs.yaml`
2. Hugo builds the site into `public/`
3. `actions/upload-pages-artifact` uploads `public/` as the Pages artifact
4. `actions/deploy-pages` publishes that artifact to GitHub Pages

### Repository Setting

GitHub Pages must be configured to deploy from GitHub Actions.

### Verification

After merging a docs change to `main`, confirm that the `Docs` workflow succeeds and that the site is available at `https://skyoo2003.github.io/kvs/`
