---
title: "Compatibility"
weight: 6
---

A `v1` tag is a promise: what this page lists will not break until `v2`, and what it does not
list may change in any release. This is the page to read before pinning kvs in something you
have to keep running.

Versions follow [Semantic Versioning](https://semver.org/). Within `v1.x.y`, a new minor
release may add to the list below and a patch release may only fix behaviour that already
contradicts it.

## What v1 covers

### The wire protocols

All three protocols share one keyspace, and that stays true for the type all three can carry: a
string written over HTTP is readable over RESP and gRPC, and the reverse. RESP's lists, hashes,
sets, and sorted sets have no HTTP or gRPC representation, so asking either for a key holding
one is an error rather than a value.

| Protocol | Promised | Details |
|---|---|---|
| HTTP | `PUT`, `GET`, `DELETE` on `/v1/keys/{key}`, and `/healthz` — paths, methods, request and response bodies, status codes | [HTTP API](../http-api/) |
| gRPC | The service defined by the protobuf files under `api/kvsv1`, and the standard gRPC health service | `api/kvsv1` |
| RESP2 | The commands listed on the Redis API page, with the replies documented there | [Redis API](../redis-api/) |

For gRPC the `.proto` file is the contract. The Go identifiers `protoc` generates from it
follow `protoc`'s own rules, so they are promised only insofar as the proto is.

A RESP command that is not on the Redis API page is not promised, even if the server happens
to answer it.

### The command line

Every flag on the [CLI Usage](../cli/) page keeps its name, its meaning, and its default. Each
`kvs serve` setting keeps the config-file key and environment variable that set it too;
`--config`, `--version`, and `--help` have neither, because the CLI answers those itself rather
than passing them to the server. `kvs serve` with no flags
keeps asking for `:3456` for HTTP, `:3457` for gRPC, and `127.0.0.1:6379` for RESP — the
addresses are the promise, not that each one binds, because a default RESP port already taken
is logged and skipped rather than being fatal. `kvs version` keeps printing a version.

New flags may be added. Existing ones will not change under you.

### The Go library

Importing `github.com/skyoo2003/kvs` gets you:

- `Store` and its constructors `NewStore` and `Open`, with `Get`, `Put`, `Delete`, `Read`,
  `Write`, `Snapshot`, `Speculate`, `Watch`, `SetCodec`, and `Close`
- The transaction types `ReadTx` and `Tx`, and `Watch`
- `Entry`, and the `Codec` interface with `StringCodec`
- The sentinel errors `ErrKeyNotFound`, `ErrNoCodec`, `ErrUnsupportedValue`, and `ErrNotLeader`
  with `NotLeaderError`

The exact signatures live in [`testdata/api-surface.txt`][surface], which is generated from
the source and compared against it by a test on every run. Nothing can join or leave that file
without the change showing up in review.

[surface]: https://github.com/skyoo2003/kvs/blob/main/testdata/api-surface.txt

## What v1 does not cover

**Cluster plumbing on `Store`.** `SetReplicator`, `ReplaceWith`, and `ApplyReplicated` are
exported so `internal/cluster` can reach them across the package boundary, not for callers
importing this package. They may change or disappear in a minor release.

**`github.com/skyoo2003/kvs/pkg/resp`.** It exists so the server can speak RESP2, not as a
RESP library for other programs. The protocol kvs answers on the wire is promised; this Go
package is not.

**Anything under `internal/`.** The Go toolchain already stops you importing it; this is the
same statement in words.

**The on-disk format.** A data directory carries a `format` file naming the version that wrote
it, and kvs refuses to start on a directory whose version it does not recognize rather than
reading it and hoping. That refusal is the promise; the contents are not. Data written by one
release is not promised to be readable by another, and kvs does not convert between formats. An
upgrade that raises the format means: drain, start the new version against an empty directory,
load the data again.

**Performance.** Throughput, latency, and memory are not part of the promise. kvs is built for
not losing writes, not for being fast, and a release may trade one for the other.

**The Go version.** kvs builds with the Go release named in `go.mod`. A minor release may
require a newer one.

## Breaking it anyway

Something on this page can only be removed or changed in `v2`. Before that it gets deprecated
in a `v1.x` release — still working, marked in its documentation and in the release notes —
and stays that way until the next major.

One exception: a fix for a security vulnerability may break a promise in a minor release. When
that happens the release notes say so explicitly, in those words.

## Trust boundary

**kvs has no authorization and no TLS, and only one of its three listeners can ask who you
are.** Set `resp_password` in the config file or `KVS_RESP_PASSWORD` in the environment — there
is deliberately no flag, because a flag puts the password in the process list — and the RESP
listener requires `AUTH` before it answers anything, cluster joins included, since `--join` goes
through that listener rather than a second port with its own credentials. HTTP and gRPC have
nothing of the kind: anyone who can open a connection to either can read, change, and delete
everything in the keyspace, and no listener encrypts anything, so a RESP password crosses the
wire in the clear.

The defaults reflect that unevenly, and it is worth knowing which is which:

| Listener | Default | Reachable from | Can require a password |
|---|---|---|---|
| HTTP | `:3456` | every interface | No |
| gRPC | `:3457` | every interface | No |
| RESP | `127.0.0.1:6379` | loopback only | Yes, with `resp_password` |

RESP defaults to loopback because port 6379 is scanned continuously across the public
internet. HTTP and gRPC do not, so a kvs started with defaults on a machine with a public
address is exposed on two ports.

Run kvs on a network you trust, or put something that authenticates in front of it. Do not put
a listener on a public address.

This is a description of v1, not a permanent position. Giving the other two listeners the same
option adds to the promise rather than breaking it, so it can arrive in a `v1.x` release.

## Further Reading

- [Overview](../overview/) — installation and library usage
- [CLI Usage](../cli/) — every flag, config key, and environment variable
- [HTTP API](../http-api/) — REST endpoint details
- [Redis API](../redis-api/) — supported RESP commands and behaviour notes
- [Durability and Clustering](../clustering/) — what `--data-dir` and `--raft-addr` promise
- [Release Process](../release/) — how a version gets cut
