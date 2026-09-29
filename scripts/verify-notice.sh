#!/usr/bin/env sh
# Verify that NOTICE inventories every non-main module linked into kvs.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
actual=$(mktemp)
recorded=$(mktemp)
trap 'rm -f "$actual" "$recorded"' EXIT

cd "$root"
go list -deps -json ./cmd/kvs |
  awk '
    /"Path":/ { path = $2; gsub(/[",]/, "", path) }
    /"Version":/ { version = $2; gsub(/[",]/, "", version) }
    /"Main": true/ { main = 1 }
    /^}/ {
      if (path != "" && version != "" && !main) print path "@" version
      path = version = ""; main = 0
    }
  ' | sort -u > "$actual"

sed -n '/^Third-party components$/,/^Upstream notices$/p' NOTICE |
  sed -n 's/^- \([^ ]*\) — .*/\1/p' | sort -u > "$recorded"

if ! diff -u "$recorded" "$actual"; then
  echo "NOTICE's third-party component inventory is stale." >&2
  exit 1
fi
