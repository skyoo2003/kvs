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

## Documentation Site Release

The static documentation site is built with Hugo and published to the `gh-pages` branch.

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
3. `JamesIves/github-pages-deploy-action` publishes `public/` to the `gh-pages` branch

### Repository Setting

GitHub Pages must be configured to serve from the `gh-pages` branch root.

### Verification

After merging a docs change to `main`, confirm that the `Docs` workflow succeeds and that the site is available at `https://skyoo2003.github.io/kvs/`
