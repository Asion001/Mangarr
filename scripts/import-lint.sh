#!/usr/bin/env sh
# Only module implementations (internal/modules/<kind>/<impl>) may talk to
# vendor APIs. The core must depend on module interfaces, never on a
# concrete implementation package.
set -eu
cd "$(dirname "$0")/.."

impls=$(find internal/modules -mindepth 2 -maxdepth 2 -type d | grep -v -E 'internal/modules/(httpx|all)$' || true)
fail=0
for impl in $impls; do
  pkg="github.com/Asion001/mangarr/$impl"
  # allowed importers: the implementation itself, modules/all, tests and testutil
  offenders=$(grep -rl --include='*.go' "\"$pkg\"" cmd internal \
    | grep -v "^$impl/" | grep -v '^internal/modules/all/' | grep -v '_test.go$' | grep -v '^internal/testutil/' || true)
  if [ -n "$offenders" ]; then
    echo "core code imports module implementation $pkg:"
    echo "$offenders" | sed 's/^/  /'
    fail=1
  fi
done
exit $fail
