#!/usr/bin/env bash
set -uo pipefail
go test -race ./... 2>&1
