---
title: "Release Process"
weight: 4
---

The repository tracks release notes in `docs/RELEASE.md` and automates tagged releases with GitHub Actions.

## Commands

```sh
$ changie new
$ changie batch {version}
$ changie merge
```

## Current Workflow

The release workflow lives in `.github/workflows/release.yaml` and runs when a tag matching `v*` is pushed.

## Note

This page summarizes the current release information and workflow already present in the repository.
