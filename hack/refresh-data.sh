#!/usr/bin/env sh
# Rewrites internal/data/*.yaml from endoflife.date and Pluto.
set -eu
cd "$(dirname "$0")/.."
go run ./hack/refresh-data internal/data
