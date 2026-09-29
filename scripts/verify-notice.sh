#!/usr/bin/env sh
# Verify that NOTICE inventories every non-main module linked into kvs.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
actual=$(mktemp)
recorded=$(mktemp)
licenses=$(mktemp -d)
trap 'rm -f "$actual" "$recorded"; rm -rf "$licenses"' EXIT

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

# A component list alone is not enough for licenses such as BSD-3-Clause:
# binary distributions must reproduce their copyright and license terms. The
# collector fails if any linked module lacks the material we package at release.
"$root/scripts/collect-third-party-licenses.sh" "$licenses/THIRD_PARTY_LICENSES"
