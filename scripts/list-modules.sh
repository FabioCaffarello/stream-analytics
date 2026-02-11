#!/usr/bin/env bash
set -euo pipefail

# Prints module directories from go.work, one per line.
go work edit -json | jq -r '.Use[].DiskPath'
