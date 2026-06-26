#!/usr/bin/env bash
# Static analysis: go vet + golangci-lint. Pass extra args through (e.g. --fix).
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_tool golangci-lint

section "lint: go vet"
go vet ./...

section "lint: golangci-lint"
golangci-lint run "$@" ./...
