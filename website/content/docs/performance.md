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
26.6.2, running on **battery power**, with Go 1.26.7 and memtier_benchmark 2.5.1, on the change
that added this page, on top of `2a8076a`. Laptop numbers move with temperature and power
source; the two runs per mode below are there to show by how much.

The load is `make memtier`: 4 threads × 50 connections, one `SET` to every ten `GET`s, 32-byte
values over 100,000 random keys, 60 seconds. Latencies are in milliseconds and are what the client
saw, queueing included.

## Under load

| Mode | Run | Ops/sec | SET p50 | SET p99 | GET p50 | GET p99 |
|---|---|---:|---:|---:|---:|---:|
| memory | 1 | 213,819 | 0.87 | 2.90 | 0.85 | 2.66 |
| memory | 2 | 205,800 | 0.91 | 2.93 | 0.89 | 2.74 |
| durable | 1 | 2,289 | 758 | 2,015 | 3.98 | 12.1 |
| durable | 2 | 2,786 | 758 | 774 | 3.98 | 4.67 |
| cluster | 1 | 472 | 4,555 | 4,653 | 0.087 | 0.143 |
| cluster | 2 | 469 | 4,522 | 4,686 | 0.079 | 0.143 |

**memory** is `kvs serve` with no `--data-dir`: about **210,000 operations a second** at under a
millisecond at the median.

**durable** adds `--data-dir`, and every write is flushed to disk before it is acknowledged,
under the store's one write lock. On this machine a flush takes about 3.7ms, so writes top out
near 270 a second however many clients send them, and two hundred connections queue behind
each other for **three quarters of a second** at the median. Reads are cheap but wait for
whichever flush holds the lock, which is the 4ms at their median. The first run's 2-second SET
tail did not repeat; the machine was on battery.

**cluster** is three nodes on this one machine with the load on the leader. Every write is a
consensus round, one at a time, at about 21ms each (below) — roughly **45 writes a second**, and
four and a half seconds of queueing at the median under this many connections. Reads never touch
consensus, which is why they are the fastest reads in the table; it is also why they
[may be behind](../clustering/). The total is low because each connection waits for its own write
before sending its next read.

## Per operation

`make bench BENCH_COUNT=6`, median of the six runs:

| Benchmark | What it is | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `BenchmarkPut` | in-memory write | 101 | 144 | 2 |
| `BenchmarkPutParallel` | the same, from every core | 166 | 144 | 2 |
| `BenchmarkGet` | in-memory read | 61 | 32 | 1 |
| `BenchmarkGetParallel` | the same, from every core | 102 | 32 | 1 |
| `BenchmarkPutDurable` | write with the append log | 3,700,000 | 4,633 | 8 |
| `BenchmarkRESPSet` | `SET` through go-redis over loopback | 14,990 | 5,469 | 32 |
| `BenchmarkRESPGet` | `GET` through go-redis over loopback | 14,490 | 3,560 | 22 |
| `BenchmarkRESPSetParallel` | `SET` from every core, one client pool | 7,326 | 5,480 | 32 |
| `BenchmarkClusterPut` | write through a three-node cluster | 21,000,000 | 149,000 | 624 |

The store's parallel runs are slower per operation than its serial ones because every caller
shares its one lock; they are there so that sharding it has something to beat.

## Reproducing it

```sh
brew install memtier_benchmark redis    # memtier, and redis-cli for the readiness check
make memtier BENCH_MODE=memory          # or durable, or cluster; BENCH_TIME=60 by default
make bench BENCH_COUNT=10               # then compare two builds with benchstat
```

`make memtier` builds `dist/kvs`, starts it on port 16379 (`KVS_BENCH_PORT` moves it), waits
until it answers — in cluster mode, until the other two nodes have joined — and leaves the full
result in `dist/memtier-<mode>.json`. It stops every node it started when it exits.
