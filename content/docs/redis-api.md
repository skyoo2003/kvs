---
title: "Redis API"
weight: 40
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

### Publish and subscribe

`PSUBSCRIBE` `PUBLISH` `PUNSUBSCRIBE` `SUBSCRIBE` `UNSUBSCRIBE`

## Behaviour worth knowing

These are the places kvs answers correctly but not identically to Redis.

**One keyspace.** `SELECT` accepts only index 0, and `FLUSHDB` and `FLUSHALL` do the same
thing.

**RESP2 only.** `HELLO 3` answers `NOPROTO`, which every client reads as a signal to fall
back. go-redis asks for RESP3 by default and downgrades on its own.

**`SCAN` cursors are opaque handles, not offsets.** The keyspace walk is paged and honours
`COUNT`, and a cursor identifies the last key a call reached rather than a position, so
deleting keys the walk has already passed cannot make it skip one. Because the handle lives
on the connection, a cursor from a different connection reads as a finished iteration rather
than an error.

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

**`CONFIG SET` is refused** rather than accepted and ignored. `CONFIG GET` answers for the
parameters clients probe on connect.

## Not implemented

Replication, cluster mode, RDB and AOF persistence, Lua scripting (`EVAL`), functions,
keyspace notifications, streams, blocking commands (`BLPOP` and friends), ACLs beyond a
single password, RESP3 push and attribute types, `MONITOR`, `OBJECT`, bit operations, `GEO`,
and HyperLogLog.

`ZADD` with `INCR` and the `NX`, `XX`, `GT`, and `LT` options on `EXPIRE` are also absent.
Anything unimplemented answers with an error, so a client discovers it rather than getting a
wrong result.
