---
title: "Performance"
weight: 6
---

These numbers are a baseline to compare later changes against, not a promise:
[performance is outside the compatibility promise](../compatibility/), and kvs is built for not
losing writes before it is built for being fast. What the page does promise is that every number
on it can be reproduced with one command from a checkout.

## How it was measured

One machine, nothing else under load: an Apple M4 (10 cores, 16 GB, internal SSD) on macOS
26.6.2, on AC power, with Go 1.26.7 and memtier_benchmark 2.5.1, on the change that last
updated this page, on top of `f57bc5b`. The two runs per mode below are there to show how far
a run moves on its own.

The load is `make memtier`: every one of the 100,001 keys is written first, so reads hit rather
than miss, then 4 threads × 50 connections send one `SET` to every ten `GET`s, 32-byte values
at random keys, for 60 seconds. Latencies are in milliseconds and are what the client saw,
queueing included.

## Under load

| Mode | Run | Ops/sec | SET p50 | SET p99 | GET p50 | GET p99 |
|---|---|---:|---:|---:|---:|---:|
| memory | 1 | 210,137 | 0.89 | 2.83 | 0.86 | 2.70 |
| memory | 2 | 204,953 | 0.92 | 2.80 | 0.90 | 2.69 |
| durable | 1 | 2,754 | 758 | 803 | 3.98 | 5.98 |
| durable | 2 | 2,773 | 754 | 934 | 3.98 | 7.71 |
| cluster | 1 | 437 | 4,850 | 5,014 | 0.055 | 0.119 |
| cluster | 2 | 435 | 4,882 | 5,145 | 0.055 | 0.143 |

**memory** is `kvs serve` with no `--data-dir`: about **210,000 operations a second** at under a
millisecond at the median.

**durable** adds `--data-dir`, and every write is flushed to disk before it is acknowledged,
under the store's one write lock. On this machine a flush takes about 3.7ms, so writes top out
near 270 a second however many clients send them, and two hundred connections queue behind
each other for **three quarters of a second** at the median. Reads are cheap but wait for
whichever flush holds the lock, which is the 4ms at their median.

**cluster** is three nodes on this one machine with the load on the leader. Every write is a
consensus round, one at a time, at about 22ms each (below) — roughly **43 writes a second**, and
nearly five seconds of queueing at the median under this many connections. The total is low
because each connection waits for its own write before sending its next read, so the node is
idle most of the time, and that is why its reads are the fastest in the table: nothing is
queued ahead of them. They are answered locally without asking the leader, which is also why
they [may be behind](../clustering/).

## Per operation

`make bench BENCH_COUNT=6`, median of the six runs. The RESP rows come from a second run of that
package alone, after something else on the machine ran through the first:

| Benchmark | What it is | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `BenchmarkPut` | in-memory write | 103 | 144 | 2 |
| `BenchmarkPutParallel` | the same, from every core | 165 | 144 | 2 |
| `BenchmarkGet` | in-memory read | 62 | 32 | 1 |
| `BenchmarkGetParallel` | the same, from every core | 106 | 32 | 1 |
| `BenchmarkPutDurable` | write with the append log | 3,750,000 | 4,662 | 8 |
| `BenchmarkRESPSet` | `SET` through go-redis over loopback | 15,070 | 5,469 | 32 |
| `BenchmarkRESPGet` | `GET` through go-redis over loopback | 14,490 | 3,560 | 22 |
| `BenchmarkRESPSetParallel` | `SET` from every core, one client pool | 9,690 | 5,481 | 32 |
| `BenchmarkClusterPut` | write through a three-node cluster | 22,200,000 | 145,000 | 620 |

The store's parallel runs are slower per operation than its serial ones because every caller
shares its one lock; they are there so that sharding it has something to beat.

## Reproducing it

```sh
brew install memtier_benchmark redis    # memtier, and redis-cli for the readiness check
make memtier BENCH_MODE=memory          # or durable, or cluster; BENCH_TIME=60 by default
make bench BENCH_COUNT=10               # then compare two builds with benchstat
```

`make memtier` builds `dist/kvs`, starts it on port 16379 (`KVS_BENCH_PORT` moves it), waits
until it answers — in cluster mode, until the other two nodes have joined — writes every key
once, and leaves the full result in `dist/memtier-<mode>.json`. It stops every node it started
when it exits.
