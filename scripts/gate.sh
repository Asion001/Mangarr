#!/usr/bin/env sh
# What CI checks, run here: formatting, vet, the import rule and the tests
# on both databases. Postgres comes from MANGARR_TEST_POSTGRES when it is
# set (scripts/pg-test.sh starts one).
set -eu
cd "$(dirname "$0")/.."
echo "== gofmt"
test -z "$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
echo "== vet"
CGO_ENABLED=${CGO_ENABLED:-0} go vet ./...
echo "== imports"
./scripts/import-lint.sh
echo "== tests"
CGO_ENABLED=${CGO_ENABLED:-0} go test -count=1 "$@" ./...
