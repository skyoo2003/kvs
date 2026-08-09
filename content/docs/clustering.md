---
title: "Durability and Clustering"
weight: 5
---

kvs keeps everything in memory unless you ask it not to. `--data-dir` makes the keyspace
survive a restart; `--raft-addr` on top of that makes it survive losing a machine. Both are
off by default, so a single in-memory node is still what `kvs serve` gives you.

This page is about what those two flags promise and, more usefully, what they do not.

## Durability

Point `--data-dir` at a directory and every change is appended to a log there and replayed at
startup, across all three protocols.

```sh
$ kvs serve --data-dir /var/lib/kvs
$ redis-cli -p 6379 set greeting hello
OK
# restart kvs
$ redis-cli -p 6379 get greeting
"hello"
```

**A write that has returned is on disk.** The log is flushed and `fsync`ed before the command
answers, so an acknowledged write survives a crash and not only a clean shutdown. The cost is
that writes go no faster than the disk can sync.

**A crash loses the record that was in flight and nothing before it.** A log whose last record
was cut short stops being read there, kvs says how many bytes it dropped, and startup rewrites
the file without them.

**The log is compacted at startup, not while running.** The replay leaves the whole live
keyspace in memory, so rewriting the log costs nothing extra at that moment and needs no
background worker. A long-running process grows its log without bound in the meantime.

**One node means one disk.** Durability is not availability: a lost disk is lost data, and a
stopped process is a stopped service. That is what clustering is for.

## Clustering

`--raft-addr` puts a node in a [Raft](https://raft.github.io/) cluster. Writes go to whoever is
leader and are acknowledged only once a majority of nodes have them. If the leader dies the rest
elect a new one and writes carry on with nobody doing anything.

```sh
# first node — starts the cluster
$ kvs serve --data-dir /var/lib/kvs1 --raft-addr 127.0.0.1:7901 --resp-addr 127.0.0.1:6381

# the others — join through any node already in
$ kvs serve --data-dir /var/lib/kvs2 --raft-addr 127.0.0.1:7902 --resp-addr 127.0.0.1:6382 \
            --join 127.0.0.1:6381
$ kvs serve --data-dir /var/lib/kvs3 --raft-addr 127.0.0.1:7903 --resp-addr 127.0.0.1:6383 \
            --join 127.0.0.1:6381
```

`--join` takes the **Redis address** of a node already in the cluster, not its Raft address:
joining reuses the RESP listener through a `KVS.JOIN` command, so there is no second port and no
second way to authenticate. A joining node keeps asking until it is let in, because the node it
asks may not have elected anyone yet.

### What it promises

**An acknowledged write survives losing a minority.** A majority has it on disk before the
command answers, so any surviving majority still has it. This is strong consistency, not
eventual: the price is that every write pays one consensus round.

**Failover needs no person.** Stop the leader and the survivors elect a replacement on their
own.

### What it does not

**Three nodes, not two.** A majority of two is two, so a two-node cluster stops accepting
writes the moment either node goes down — strictly worse than one node. Run an odd number,
three or more.

**Writes stop during an election.** On a three-node cluster on one machine, writes came back
between **1.3 and 3.0 seconds** after the leader was stopped across eight runs. There is no way
around the gap; the cluster is deciding who may accept writes, and accepting them meanwhile is
how two nodes end up disagreeing. Expect longer over a real network.

**Reads may be behind.** Any node answers reads, from its own copy, without asking the leader.
A follower that is catching up — or one cut off from the majority entirely — will happily serve
a value the leader has already replaced. If you need the current value, read from the leader.

**Only the leader takes writes,** and each protocol says so its own way:

| Protocol | Reply | Carries the leader's address |
|---|---|---|
| RESP | `MOVED 0 <leader>`, or `CLUSTERDOWN` during an election | Yes — the wording Redis Cluster uses, so clients already follow it |
| HTTP | `409 Conflict` | In the `error` message text only |
| gRPC | `FAILED_PRECONDITION` | In the status message text only |

The address a client is sent to is the node's `--node-id`, which defaults to its `--resp-addr`.
Set it explicitly if clients reach the node by some other name.

**Publish/subscribe does not cross nodes.** `PUBLISH` reaches the subscribers connected to that
one node. Channels are not keyspace, so they are not replicated, and a `PUBLISH` on the leader
returns `0` while a subscriber sits on a follower. Point publishers and subscribers at the same
node, or use the keyspace.

**`INFO` names the leader; `redis_mode` does not.** A follower reports `role:slave` with
`master_host` and `master_port`, and the leader reports `role:master` with the others counted in
`connected_slaves`, so a client can find where writes go by asking rather than by being refused.
`redis_mode` stays `standalone` and `cluster_enabled` stays `0` on every node: those name the
protocol, and kvs is not Redis Cluster — there are no hash slots to ask about.

**`/healthz` knows nothing about the cluster.** It answers `200` whenever the process is
listening, and the gRPC health service always reports `SERVING`. A node that has lost contact
with the majority and cannot accept a single write still looks healthy to both. They are
liveness checks; treat them as "this process is up", not "this node is useful".

**No sharding.** Every node holds the whole keyspace, so the cluster is as large as one node can
hold. Writes are serialized through the leader, one consensus round each: this is built for
staying up, not for throughput.

## Configuration notes

**`--data-dir` is required in a cluster.** The Raft log has to live somewhere. It replaces the
single-node append log rather than adding to it — the same changes written twice would only be
two things to keep in step.

**The data directory carries a format version.** kvs writes a `format` file into `--data-dir`
the first time it uses one, and refuses to start on a directory whose version it does not
recognize — including one written before that file existed. The alternative is a replay reading
bytes laid out by another version, which surfaces much later and looks like corruption rather
than a version mismatch. There is no conversion: move the directory aside and load the data
again. The version covers the Raft store too, so it moves when the consensus library changes
its own layout.

**`--node-id` is required when there is no RESP listener.** It defaults to `--resp-addr`, so
`--resp-addr none` leaves nothing to borrow and `serve` refuses to start rather than join a
cluster under a blank name.

**`--raft-addr` has to be an address, not just a port.** The other nodes are given it to dial, so
a bare `:7901` is refused at startup — `local bind address is not advertisable` — where
`127.0.0.1:7901` or `10.0.0.1:7901` is accepted. Give it the address those nodes can reach this
one on, which inside a container is rarely the one it binds.

**Containers need the Raft port published.** The published image exposes 3456, 3457, and 6379;
a clustered node also has to be reachable on whatever `--raft-addr` you give it, and by an
address the other nodes can actually resolve.

## Further Reading

- [CLI Usage](../cli/) — every flag, config key, and environment variable
- [Redis API](../redis-api/) — supported RESP commands and behaviour notes
- [HTTP API](../http-api/) — REST endpoint details
- [Overview](../overview/) — library usage and installation
