#!/usr/bin/env bash
set -euo pipefail

echo "=== 4x verification ==="

echo "--- Go version ---"
go version

echo "--- Build ---"
go build ./cmd/4x
echo "PASS: build"

echo "--- Vet ---"
go vet ./...
echo "PASS: vet"

echo "--- Test ---"
go test ./...
echo "PASS: test"

echo "--- Binary check ---"
./bin/4x --version
echo "PASS: binary runs"

echo "=== All checks passed ==="
