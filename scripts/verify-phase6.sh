#!/usr/bin/env bash
set -euo pipefail

go build ./...
go test ./... -count=1
go test ./internal/workspace -run TestInitCreatesEmptyCatalogFiles -count=1
go test -race ./...
make lint
make vet
