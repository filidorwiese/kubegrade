#!/usr/bin/env sh
# Re-vendors Pluto versions.yaml into internal/data.
set -eu
cd "$(dirname "$0")/.."
go run ./hack/refresh-data internal/data
