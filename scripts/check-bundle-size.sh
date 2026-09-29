#!/bin/sh
# Fail if the main JS bundle exceeds 80 KB gzip (CLAUDE.md resource budget).
set -eu
limit=${BUNDLE_LIMIT_BYTES:-81920}
dir=$(dirname "$0")/../web/dist/assets
total=0
for f in "$dir"/*.js; do
  [ -e "$f" ] || { echo "no JS bundle found in $dir" >&2; exit 1; }
  size=$(gzip -9 -c "$f" | wc -c)
  total=$((total + size))
done
echo "JS gzip total: $total bytes (limit $limit)"
[ "$total" -le "$limit" ] || { echo "bundle too large" >&2; exit 1; }
