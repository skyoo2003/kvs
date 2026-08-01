---
title: "Redis API"
weight: 4
---

# Redis and Valkey compatible API

kvs speaks RESP2, the protocol Redis and Valkey clients use, so existing tooling connects
without a shim: `redis-cli`, `redis-benchmark`, `go-redis`, and any other RESP2 client.

All three protocols share one keyspace. A key written over RESP is readable over the HTTP
and gRPC APIs and the other way round.

```sh
$ redis-cli -p 6379 set greeting hello
OK
$ curl http://localhost:3456/v1/keys/greeting
{"key":"greeting","value":"hello"}
```

## Listening

The RESP listener defaults to **`127.0.0.1:6379`**, unlike the HTTP and gRPC listeners which
bind every interface. Port 6379 is scanned continuously across the public internet and kvs
has no authentication unless you configure one, so exposing it is a deliberate step.

```sh
$ kvs serve                              # RESP on 127.0.0.1:6379
$ kvs serve --resp-addr :6379            # every interface
$ kvs serve --resp-addr none             # RESP disabled
```

If something already holds the default port — a local Redis, say — kvs logs a warning, leaves
RESP off, and serves HTTP and gRPC as usual. An address you name yourself is different: kvs
refuses to start rather than quietly ignore it.

The server holds at most **10000** connections at once, the Redis `maxclients` default, and
answers `ERR max number of clients reached` past that. A client that connects and sends
nothing within 30 seconds is dropped; once it has sent a command it may idle indefinitely,
which is what a subscriber does.

### Authentication

Set a password to require `AUTH`. There is deliberately **no command line flag**: an
argument is visible to anything that can list processes. Use the config file or the
environment instead.

```sh
$ KVS_RESP_PASSWORD=s3cret kvs serve --resp-addr :6379
$ redis-cli -p 6379 -a s3cret ping
PONG
```

```yaml
# config.yaml
resp_addr: ":6379"
resp_password: "s3cret"
```

### Containers

The image publishes 6379, but the loopback default still applies inside the container, so
set the address when you publish the port:

```sh
docker run -p 6379:6379 -e KVS_RESP_ADDR=:6379 -e KVS_RESP_PASSWORD=s3cret \
  ghcr.io/skyoo2003/kvs:latest-alpine
```

## Supported commands

### Connection and server

`AUTH` `CLIENT` (`ID`, `GETNAME`, `SETNAME`, `SETINFO`, `INFO`) `COMMAND` `CONFIG GET`
`ECHO` `HELLO` `INFO` `PING` `QUIT` `RESET` `SELECT`

### Strings

`APPEND` `DECR` `DECRBY` `GET` `GETDEL` `GETEX` `GETRANGE` `GETSET` `INCR` `INCRBY`
`INCRBYFLOAT` `MGET` `MSET` `MSETNX` `PSETEX` `SET` `SETEX` `SETNX` `SETRANGE` `STRLEN`

`SET` accepts `NX`, `XX`, `GET`, `KEEPTTL`, `EX`, `PX`, `EXAT`, and `PXAT`.

### Keys and expiry

`COPY` `DBSIZE` `DEL` `EXISTS` `EXPIRE` `EXPIREAT` `FLUSHALL` `FLUSHDB` `KEYS` `PERSIST`
`PEXPIRE` `PEXPIREAT` `PTTL` `RANDOMKEY` `RENAME` `RENAMENX` `SCAN` `TTL` `TYPE` `UNLINK`

### Hashes

`HDEL` `HEXISTS` `HGET` `HGETALL` `HINCRBY` `HINCRBYFLOAT` `HKEYS` `HLEN` `HMGET` `HSCAN`
`HSET` `HSETNX` `HVALS`

### Lists

`LINDEX` `LLEN` `LPOP` `LPUSH` `LPUSHX` `LRANGE` `LREM` `LSET` `LTRIM` `RPOP` `RPUSH`
`RPUSHX`

### Sets

`SADD` `SCARD` `SDIFF` `SINTER` `SISMEMBER` `SMEMBERS` `SPOP` `SRANDMEMBER` `SREM` `SSCAN`
`SUNION`

### Sorted sets

`ZADD` `ZCARD` `ZCOUNT` `ZINCRBY` `ZMSCORE` `ZRANGE` `ZRANGEBYSCORE` `ZRANK` `ZREM`
`ZREVRANGE` `ZREVRANGEBYSCORE` `ZREVRANK` `ZSCAN` `ZSCORE`

`ZADD` accepts `NX`, `XX`, `CH`, `GT`, and `LT`.

### Transactions

`DISCARD` `EXEC` `MULTI` `UNWATCH` `WATCH`

`WATCH` tracks the keys you name, so a write elsewhere in the keyspace does not abort the
transaction. `EXEC` runs the whole batch under one lock and encodes its replies before
releasing it, so a slow client cannot stall other writers.

One transaction may queue up to **64 MiB** of commands. A queue holds every command until `EXEC`
runs it, so the budget is what keeps one connection from queueing until the process runs out of
memory. Each argument is charged its bytes plus a fixed overhead, because a command carrying a
million empty arguments costs the server far more than its bytes say. A command over the budget
is refused and marks the transaction, so `EXEC` answers `EXECABORT` rather than applying part of
a batch and dropping the rest.

### Publish and subscribe

`PSUBSCRIBE` `PUBLISH` `PUNSUBSCRIBE` `SUBSCRIBE` `UNSUBSCRIBE`

### Scripting

`EVAL` `EVALSHA` `SCRIPT LOAD` `SCRIPT EXISTS` `SCRIPT FLUSH`

Scripts are Lua 5.1, and a script reaches the keyspace through `redis.call` and `redis.pcall`
with `KEYS` and `ARGV` bound the way Redis binds them. `redis.error_reply`,
`redis.status_reply`, and `redis.sha1hex` are there too, and the table answers to `server` as
well, the name Valkey gives it. `redis.log` is accepted and discarded, since kvs has no script
log to write to and a script that calls it wants to keep running rather than fail.

```sh
$ redis-cli eval "return redis.call('INCRBY', KEYS[1], ARGV[1])" 1 counter 5
(integer) 5
```

`EVALSHA` answers `NOSCRIPT` for a digest the cache does not hold, which is the reply every
client library keys its fallback on: it sends the digest first and resends the script only
after seeing that code. `EVAL` caches on the way through, so that fallback needs no
`SCRIPT LOAD` of its own.

### Cluster

`KVS.JOIN <node-id> <raft-addr>` asks the node it is sent to, which has to be the leader, to
admit another node as a voting member. `kvs serve --join` sends it for you; it is on the RESP
listener rather than a port of its own so that joining authenticates the same way everything
else does.

## Behaviour worth knowing

These are the places kvs answers correctly but not identically to Redis.

**One keyspace.** `SELECT` accepts only index 0, and `FLUSHDB` and `FLUSHALL` do the same
thing.

**RESP2 only.** `HELLO 3` answers `NOPROTO`, which every client reads as a signal to fall
back. go-redis asks for RESP3 by default and downgrades on its own.

**`SCAN` cursors are opaque handles, not offsets.** The keyspace walk is paged and honours
`COUNT`, and a cursor identifies the last key a call reached rather than a position, so
deleting keys the walk has already passed cannot make it skip one. The handle lives on the
server rather than on the connection, because client libraries pool connections: the `SCAN` that
opens an iteration and the one that continues it routinely land on different sockets.

The server remembers **1024** unfinished iterations. Past that, opening a new one forgets the
handle that has sat still longest, so an abandoned walk is dropped before a live one, and a
forgotten cursor answers `ERR invalid cursor`. That is
deliberately an error rather than an empty final page: `0` with no keys is the protocol's signal
that a walk is complete, so answering it there would tell a client it had enumerated a keyspace
it had barely started. A client that sees it should start the iteration again.

`HSCAN`, `SSCAN`, and `ZSCAN` page the same way over their own elements. `ZSCAN` walks a
sorted set by member name rather than by score, because a cursor has to resume in an order
that does not move when a score changes; the SCAN family promises no ordering, so nothing a
client is entitled to is lost.

**Expiry is reclaimed by sampling.** An expired key stops being visible immediately, and its
memory is released either when a write touches that key or when a write's sampling sweep
reaches it. Each write inspects a bounded sample of the keys that carry an expiry, so a
keyspace with no expiries pays nothing and an abandoned key does not linger.

**A slow subscriber is dropped.** Messages queue per connection under both a count and a
memory budget, and a subscriber that exceeds either is disconnected rather than slowing the
publisher down, which is how Redis bounds a client output buffer.

**Publish/subscribe never leaves the node it happened on.** Channels are not keyspace, so a
cluster does not replicate them: a `PUBLISH` on one node returns `0` while a subscriber sits on
another. Point publishers and subscribers at the same node.

**In a cluster, only the leader takes writes.** A write sent anywhere else is answered
`MOVED 0 <leader>` — Redis Cluster's wording, with slot `0` always, because kvs does not shard —
so a client that already follows redirections needs no changes. During an election nobody is the
leader yet and the reply is `CLUSTERDOWN` instead. Reads are answered by any node and may be
behind the leader.

**`INFO` says which node you reached.** In a cluster the leader reports `role:master` and counts
the others as `connected_slaves`; a follower reports `role:slave` and names the leader in
`master_host` and `master_port`, so a client can find the node that takes writes without sending
one first. Outside a cluster it is `role:master` with no followers, as before. `HELLO` carries the
same role.

`master_link_status` is not reported: kvs does not track how far behind a follower is, and a field
invented to look complete would be worse than its absence.

**`redis_mode` stays `standalone`, and `cluster_enabled` is `0`,** on a clustered node too. Those
fields name the *protocol* a client should speak, and kvs speaks the standalone one — Redis
Cluster's mode promises hash slots and a `CLUSTER` command family that kvs does not have. A node
still answers `MOVED`, which a standalone Redis never does; the redirect is borrowed wording, not
a claim to be Redis Cluster.

**`CONFIG SET` is refused** rather than accepted and ignored. `CONFIG GET` answers for the
parameters clients probe on connect.

**A script gives up after 5 seconds.** A script holds the store's write lock from its first
instruction to its last, which is what makes it atomic, so a loop that never ends would stop
every other client for the life of the process. Redis instead lets a script run forever and
offers `SCRIPT KILL`, which works only because it can still serve a second connection while
the first sits in the interpreter. A stopped script keeps whatever it had already written, the
way Redis keeps the writes a script made before it failed.

**A script cannot reach outside the process.** Only the base, `table`, `string`, `math`, and
`cjson` libraries are open, and `dofile`, `loadfile`, `print`, and `require` are removed along
with them; `os`, `io`, `debug`, and `package` are never opened. `cmsgpack`, `bit`, and `struct`
are absent, so a script needing one of those has to be rewritten or the work moved to the
client. `redis.call` also refuses the commands that make no sense inside a script: the
transaction and subscribe families, the scripting commands themselves, and the session
commands.

`cjson.encode` and `cjson.decode` follow cjson's rules: a table whose keys are exactly 1 to n
encodes as an array and anything else, an empty table included, as an object; a JSON null
decodes to `cjson.null` rather than to nil, so it neither ends the array it sits in nor drops
the key it is under. A value with no JSON spelling, a function or a table holding itself, is an
error rather than a wrong answer. A string that is not valid UTF-8 is encoded with the
replacement character, where Redis passes the bytes through unchanged.

The script cache holds up to **16 MiB**. Past that `EVAL` still runs the script and only skips
caching it, which costs the client the `EVALSHA` shortcut rather than the command, while
`SCRIPT LOAD` reports an error because caching is all it was asked to do. `SCRIPT FLUSH`
releases the budget.

## Not implemented

Functions (`FCALL`), keyspace notifications, streams, blocking commands (`BLPOP` and friends),
ACLs beyond a single password, RESP3 push and attribute types, `MONITOR`, `OBJECT`, bit
operations, `GEO`, and HyperLogLog.

**Redis's replication and cluster commands** are absent — `REPLICAOF`, `SLAVEOF`, `WAIT`, and
the `CLUSTER` family. kvs does replicate and does cluster, through Raft and its own `KVS.JOIN`,
but none of it is driven by Redis's commands, and it does not shard, so there are no slots to
report. See [Durability and Clustering](../clustering/).

**RDB and AOF files** are absent as formats. `--data-dir` keeps an append log of its own that
serves the same purpose, and there is no `SAVE`, `BGSAVE`, or `BGREWRITEAOF` to drive it: the log
is written as part of each write and compacted at startup.

`SCRIPT KILL` and the `_RO` script variants are absent too, as is the `cmsgpack` library inside
a script.

`ZADD` with `INCR` and the `NX`, `XX`, `GT`, and `LT` options on `EXPIRE` are also absent, as is
`LIMIT` on `ZRANGEBYSCORE` and `ZREVRANGEBYSCORE`. `ZRANGE` takes positions only, so its
`BYSCORE`, `BYLEX`, and `REV` forms are absent too; `ZRANGEBYSCORE` and `ZREVRANGE` cover them.
Anything unimplemented answers with an error, so a client discovers it rather than getting a
wrong result.
