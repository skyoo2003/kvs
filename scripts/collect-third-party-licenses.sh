#!/usr/bin/env sh
# Collect the license and notice files for every module linked into kvs.
# The output is intentionally built from the module cache so its contents match
# the exact module versions selected by go.mod/go.sum.
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 OUTPUT_DIRECTORY" >&2
  exit 2
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output=$1

case "$output" in
  ''|/|.)
    echo "refusing unsafe output directory: $output" >&2
    exit 2
    ;;
esac

if [ -e "$output" ]; then
  echo "output directory already exists: $output" >&2
  exit 2
fi

modules=$(mktemp)
trap 'rm -f "$modules"' EXIT

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
  ' | sort -u > "$modules"

mkdir -p "$output"
{
  echo "Third-party license bundle for KVS"
  echo
  echo "This directory contains the license and notice files distributed by the"
  echo "exact Go modules linked into this KVS binary. See ../NOTICE for the"
  echo "component inventory."
  echo
  echo "Modules"
  echo "-------"
  sed 's/^/- /' "$modules"
} > "$output/README"

while IFS= read -r module; do
  path=${module%@*}
  version=${module##*@}
  module_dir=$(go list -m -f '{{.Dir}}' "$path")
  destination="$output/$path@$version"
  found=0

  mkdir -p "$destination"
  for file in "$module_dir"/LICENSE* "$module_dir"/COPYING* "$module_dir"/NOTICE*; do
    if [ -f "$file" ]; then
      cp "$file" "$destination/"
      found=1
    fi
  done

  if [ "$found" -eq 0 ]; then
    echo "no top-level license or notice file found for $module" >&2
    exit 1
  fi
done < "$modules"
