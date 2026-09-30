#!/usr/bin/env sh
# Run memtier_benchmark against the kvs in dist/, started in one of three shapes: memory (no
# data dir), durable (append log, one fsync per write), or cluster (three Raft nodes on this
# machine, load on the leader). Usage: memtier.sh [memory|durable|cluster] [seconds]
set -eu

mode=${1:-durable}
seconds=${2:-60}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
bin="$root/dist/kvs"
# Away from 6379 so a local Redis does not end up being what gets measured.
port=${KVS_BENCH_PORT:-16379}
work=$(mktemp -d)
pids=""

cleanup() {
  for pid in $pids; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT
# A trapped signal only runs its handler and then carries on, so exit here and let EXIT clean up.
trap 'exit 1' INT TERM

for tool in memtier_benchmark redis-cli; do
  command -v "$tool" >/dev/null || { echo "$tool not found on PATH" >&2; exit 1; }
done

# kvs reads KVS_* from the environment for any flag not passed, which would change the mode
# (a data dir in memory mode) or lock redis-cli out (a password).
unset KVS_DATA_DIR KVS_RAFT_ADDR KVS_JOIN KVS_NODE_ID KVS_RESP_PASSWORD

# Whatever already answers here would be what gets measured once the new node fails to bind.
if redis-cli -p "$port" PING >/dev/null 2>&1; then
  echo "something already answers on port $port; stop it or set KVS_BENCH_PORT" >&2
  exit 1
fi

# start runs node $1 on port+$1 with the remaining arguments. HTTP and gRPC take any free port:
# nothing here talks to them, and fixed ones would collide between nodes.
start() {
  i=$1
  shift
  "$bin" serve --resp-addr "127.0.0.1:$((port + i))" \
    --http-addr 127.0.0.1:0 --grpc-addr 127.0.0.1:0 "$@" >"$work/node$i.log" 2>&1 &
  pids="$pids $!"
}

# await polls INFO on the load port until it carries the given line, which is how a cluster
# says both that node 0 won the election and that the other two joined.
await() {
  tries=0
  until redis-cli -p "$port" INFO replication 2>/dev/null | tr -d '\r' | grep -qx "$1"; do
    tries=$((tries + 1))
    if [ "$tries" -gt 300 ]; then
      echo "kvs never reported $1" >&2
      cat "$work"/node*.log >&2
      exit 1
    fi
    sleep 0.1
  done
}

case $mode in
memory)
  start 0
  await role:master
  ;;
durable)
  start 0 --data-dir "$work/0"
  await role:master
  ;;
cluster)
  for i in 0 1 2; do
    set -- --data-dir "$work/$i" --raft-addr "127.0.0.1:$((port + 100 + i))"
    [ "$i" -eq 0 ] || set -- "$@" --join "127.0.0.1:$port"
    start "$i" "$@"
  done
  await connected_slaves:2
  ;;
*)
  echo "usage: $0 [memory|durable|cluster] [seconds]" >&2
  exit 2
  ;;
esac

# Every key memtier can pick, memtier-0 through memtier-$keys, is written before the load starts,
# so a GET measures a hit rather than the miss path. One MSET per thousand keys keeps this to a
# hundred writes: one SET at a time would take minutes durable and most of an hour in a cluster.
keys=100000
awk -v keys="$keys" 'BEGIN {
  value = sprintf("%032d", 0)
  for (first = 0; first <= keys; first += 1000) {
    last = first + 999
    if (last > keys) last = keys
    printf "*%d\r\n$4\r\nMSET\r\n", 1 + 2 * (last - first + 1)
    for (id = first; id <= last; id++) {
      key = "memtier-" id
      printf "$%d\r\n%s\r\n$32\r\n%s\r\n", length(key), key, value
    }
  }
}' | redis-cli -p "$port" --pipe >"$work/prefill.log" 2>&1
grep -q 'errors: 0,' "$work/prefill.log" || { cat "$work/prefill.log" >&2; exit 1; }

# Mostly reads, the way a cache or a config store is used; 32 byte values over those keys.
memtier_benchmark --server 127.0.0.1 --port "$port" --protocol redis \
  --threads 4 --clients 50 --ratio 1:10 --data-size 32 \
  --key-pattern R:R --key-maximum "$keys" --distinct-client-seed \
  --test-time "$seconds" --hide-histogram \
  --json-out-file "$root/dist/memtier-$mode.json"
