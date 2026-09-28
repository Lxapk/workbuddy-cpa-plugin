#!/bin/sh
# Run the smoke test the way CI does.
#
# Why this exists: `go test ./...` does not run smoke. It is a separate binary that
# dlopen()s the built .so and drives the plugin over the real ABI, so a stale
# assertion in it only surfaces in CI — which is exactly what happened with v0.13.51:
# the tag was pushed, the build job failed at the smoke step, and no release was
# produced. Running it locally before pushing catches that in seconds.
#
# Usage:  sh scripts/run_smoke.sh [goarch]
set -eu

GOARCH_ARG="${1:-$(go env GOARCH)}"
DIST="dist-smoke-check"

cleanup() { rm -rf "$DIST"; }
trap cleanup EXIT INT TERM

mkdir -p "$DIST"

echo "== 构建 .so ($GOARCH_ARG) =="
CGO_ENABLED=1 CC=gcc GOOS=linux GOARCH="$GOARCH_ARG" \
  go build -buildmode=c-shared -trimpath -o "$DIST/workbuddy.so" .

echo "== 构建 smoke =="
go build -o "$DIST/smoke" ./smoke

echo "== 运行 smoke =="
./"$DIST/smoke" "$DIST/workbuddy.so"
