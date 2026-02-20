# Odin M2 Evidence — Cold-Path Read Ports (2026-02-19)

## Implemented
- Cold read-port contract for snapshot queries hardened with adapter-level tests.
- `ChSnapshotReader` refactored for queryer injection, preserving pool constructor.
- Contract behavior covered: query bind path, dedup semantics, scan failure, rows failure.

## Code Anchors
- `internal/adapters/storage/clickhouse/snapshot_reader.go`
- `internal/adapters/storage/clickhouse/snapshot_reader_test.go`
- `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`

## Validation Commands
- `go test ./internal/adapters/storage/clickhouse`
- `make test MODULE=./internal/adapters/storage/clickhouse`
- `make invariants-check`
- `make docs-check`

## Result
- All commands above passed.
