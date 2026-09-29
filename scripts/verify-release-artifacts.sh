#!/usr/bin/env sh
# Verify that every released archive carries the project's and dependencies'
# license material. GoReleaser emits tar.gz archives on Unix and zip archives
# on Windows.
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 DIST_DIRECTORY" >&2
  exit 2
fi

dist=$1
found=0
modules=$(mktemp)
trap 'rm -f "$modules"' EXIT

go list -deps -json ./cmd/kvs |
  awk '
    /"Path":/ { path = $2; gsub(/[",]/, "", path) }
    /"Version":/ { version = $2; gsub(/[",]/, "", version) }
    /"Main": true/ { main = 1 }
    /^}/ {
      if (path != "" && version != "" && !main) print path "@" version
      path = version = ""; main = 0
    }
  ' | sort -u > "$modules"

check_entries() {
  archive=$1
  entries=$2
  for required in LICENSE NOTICE THIRD_PARTY_LICENSES/README; do
    if ! printf '%s\n' "$entries" | grep -Fx "$required" >/dev/null; then
      echo "$archive is missing $required" >&2
      exit 1
    fi
  done

  while IFS= read -r module; do
    if ! printf '%s\n' "$entries" | grep -F "THIRD_PARTY_LICENSES/$module/" >/dev/null; then
      echo "$archive is missing license material for $module" >&2
      exit 1
    fi
  done < "$modules"
}

for archive in "$dist"/*.tar.gz; do
  [ -e "$archive" ] || continue
  check_entries "$archive" "$(tar -tzf "$archive")"
  found=1
done

for archive in "$dist"/*.zip; do
  [ -e "$archive" ] || continue
  check_entries "$archive" "$(unzip -Z1 "$archive")"
  found=1
done

if [ "$found" -eq 0 ]; then
  echo "no release archives found in $dist" >&2
  exit 1
fi
