---
title: "Release Process"
weight: 4
---

The repository tracks release notes in `docs/RELEASE.md`, automates tagged releases with GitHub Actions, and publishes the documentation site through GitHub Pages Actions.

## Commands

```sh
$ changie new
$ changie batch {version}
$ changie merge
```

## Current Workflow

The release workflow lives in `.github/workflows/release.yaml` and runs when a tag matching `v*` is pushed.

## Project Release

For an application release, prepare the changelog with `changie`, push a version tag such as `v1.2.3`, and let the `Release` workflow publish the artifacts defined in `.goreleaser.yml`.

After the tag is pushed, verify that the `Release` workflow succeeds and that the GitHub release contains the expected binaries and archives.

## Documentation Site Release

The documentation workflow lives in `.github/workflows/docs.yaml`.

When a docs-related change is pushed to `main`, GitHub Actions builds the Hugo site, uploads the generated `public/` directory as a Pages artifact, and deploys it through GitHub Pages.

For this to publish correctly, repository settings must point GitHub Pages at GitHub Actions.

After a merge, verify that the `Docs` workflow succeeds and that the site is available at `https://skyoo2003.github.io/kvs/`.

## Note

This page summarizes the current release information and workflow already present in the repository.
