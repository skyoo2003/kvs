## [v1.0.0](https://github.com/skyoo2003/kvs/releases/tag/v1.0.0) - 2026-08-11
### Added
* Redis/Valkey compatible RESP2 server on 127.0.0.1:6379, sharing one keyspace with the HTTP and gRPC APIs; it steps aside if the port is taken, holds at most 10000 connections, and bounds one transaction's queue at 64MiB so a single client cannot exhaust memory ([#232](https://github.com/skyoo2003/kvs/issues/232))
* Lua scripting with EVAL, EVALSHA, and SCRIPT LOAD/EXISTS/FLUSH; a script runs sandboxed under one write lock, so its redis.call sequence is atomic, and gives up after 5 seconds rather than hold the store ([#232](https://github.com/skyoo2003/kvs/issues/232))
* Keep the keyspace across restarts with `kvs serve --data-dir`, which appends every change to a log and replays it at startup ([#236](https://github.com/skyoo2003/kvs/issues/236))
* The cjson library inside a script, so cjson.encode and cjson.decode work under EVAL instead of failing on a nil global; a JSON null decodes to cjson.null rather than nil, so it does not end the array it sits in ([#232](https://github.com/skyoo2003/kvs/issues/232))
* Run a Raft cluster with `kvs serve --raft-addr` and `--join`; writes pass consensus, and losing the leader triggers an election instead of needing a person ([#236](https://github.com/skyoo2003/kvs/issues/236))
* A compatibility page states what v1 promises not to break, what it leaves out, and the trust boundary kvs assumes; the exported Go surface is pinned in testdata/api-surface.txt and checked by a test, so it cannot widen or narrow unnoticed ([#244](https://github.com/skyoo2003/kvs/issues/244))
* A data directory now carries a format version, and kvs refuses to start on one it does not recognise instead of replaying bytes another version laid out; the refusal names both versions and what to do about it, and it covers the Raft store as well as the append log ([#245](https://github.com/skyoo2003/kvs/issues/245))
### Changed
* Container images now run 'serve' by default as UID 65534 and no longer declare a volume ([#228](https://github.com/skyoo2003/kvs/issues/228))
* RESP SCAN, HSCAN, SSCAN, and ZSCAN now page their walk, WATCH tracks only the keys it was given, lists cost O(1) at both ends, and expired keys are reclaimed by a sampling sweep ([#232](https://github.com/skyoo2003/kvs/issues/232))
* INFO and HELLO now report what a clustered node actually is — a follower answers role:slave and names the leader in master_host and master_port, a leader counts the others in connected_slaves, and cluster_enabled:0 says kvs is not Redis Cluster ([#236](https://github.com/skyoo2003/kvs/issues/236))
* Homebrew installs a cask instead of a formula; `brew install skyoo2003/tap/kvs` is unchanged, and the tap carries a tap_migrations.json entry so an existing formula install moves across on upgrade. GoReleaser deprecated the formula path for pre-built binaries, and the release would have broken on a floating version of it ([#248](https://github.com/skyoo2003/kvs/issues/248))
### Removed
* Drop the unused data structure packages pkg/bitset, pkg/cuckoofilter, pkg/lsm, and pkg/rbt; the storage engine went to an append log and Raft instead, and nothing in the server or the library imported them. pkg/resp stays, since the RESP server is built on it ([#236](https://github.com/skyoo2003/kvs/issues/236))
### Fixed
* Fix multi-arch (linux/arm64) release images and expose the gRPC port 3457 in container images ([#228](https://github.com/skyoo2003/kvs/issues/228))
* An HTTP 405 now carries the same JSON error body as every other HTTP error, instead of an empty response the documented contract did not allow ([#244](https://github.com/skyoo2003/kvs/issues/244))
### Documentation
* The durability and clustering page now carries measured numbers from a four hour run under load with a node stopped every thirty seconds and kept down for ten - 329,631 acknowledged writes, 111,516 of them taken while a node was gone, and none of them lost across 479 restarts - along with 51 bytes of append log per write, the Raft log a node too busy restarting never truncates, and a make soak target that reproduces all of it ([#246](https://github.com/skyoo2003/kvs/issues/246))
* The release page now covers what one tag actually produces, the checks worth running before pushing it, and an upgrade section measured by running v0.1.1 and v1 side by side - the library, the CLI, and gRPC unchanged, an HTTP 405 now carrying a JSON body, a RESP listener appearing on loopback, and no data directory to migrate because v0.1.1 never wrote one ([#248](https://github.com/skyoo2003/kvs/issues/248))
### Misc
* Migrate golangci-lint config to v2 and pin the linter to v2.12.2 in CI ([#229](https://github.com/skyoo2003/kvs/issues/229))
## [v0.1.1](https://github.com/skyoo2003/kvs/releases/tag/v0.1.1) - 2026-04-20

### Changed
* Improve open-source project infrastructure and documentation
* Align release process with existing projects (GoReleaser v2, Dockerfile security, release-drafter)
* Bump alpine from 3.14.0 to 3.23.4
* Bump actions/upload-pages-artifact from 4 to 5

### Fixed
* Fix goreleaser flag: --rm-dist → --clean for v2 compatibility
## [v0.1.0](https://github.com/skyoo2003/kvs/releases/tag/v0.1.0) - 2026-03-14
### Added
* Implement a usable kvs module and release path ([#184](https://github.com/skyoo2003/kvs/issues/184))
* Implement RBTree operations and tests ([#187](https://github.com/skyoo2003/kvs/issues/187))
* Implement in-memory LSM tree package ([#188](https://github.com/skyoo2003/kvs/issues/188))
* Implement a minimal Cobra/Viper CLI  ([#189](https://github.com/skyoo2003/kvs/issues/189))
* Add static documentation site ([#190](https://github.com/skyoo2003/kvs/issues/190))
* Add HTTP and gRPC server support ([#191](https://github.com/skyoo2003/kvs/issues/191))
### Misc
* Support Homebrew release ([#19](https://github.com/skyoo2003/kvs/issues/19))
* Fix GitHub Pages deployment ([#193](https://github.com/skyoo2003/kvs/issues/193))
