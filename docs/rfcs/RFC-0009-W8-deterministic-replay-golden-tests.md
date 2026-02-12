# RFC-0009 — W8: Deterministic Replay & Golden Tests

**Status:** Proposed
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W8 of PRD-0001
**Relates to:** ADR-0015 (Deterministic Replay), ADR-0012 (Lifecycle)

---

## 1. Goal

Build recording and replay infrastructure so that any event sequence can be captured from live data and replayed deterministically through the domain pipeline. After W8:
- `Recorder` captures live envelopes to JSON-lines fixture files
- `Player` replays fixtures through `IngestMarketData` with `FakeClock` and `ReplaySequencer`
- Golden tests validate output byte-for-byte against known-good baselines
- `cmd/consumer` supports `-record` and `-replay` flags
- No `time.Now()` calls exist in `internal/core/` (verified by CI grep check)

## 2. Scope

- Create `internal/shared/replay/` package (Recorder, Player, ReplaySequencer)
- Create fixture format (JSON-lines, one envelope per line)
- Create golden test framework (`-update-golden` flag pattern)
- Record at least one 1000-envelope fixture from live Binance stream
- Wire `-record` and `-replay` flags into `cmd/consumer`
- Add INV-R1 grep check to CI (`time.Now` in `internal/core/` = fail)

## 3. Non-Goals

- Replay from JetStream (JetStream has native replay via durable consumers)
- Full event sourcing system
- Binary fixture format (protobuf fixtures deferred to post-W6)
- Real-time replay with timing simulation

## 4. Affected Modules

| File | Action | Change |
|------|--------|--------|
| `internal/shared/replay/recorder.go` | CREATE | Wraps EventPublisher, writes envelopes to JSONL |
| `internal/shared/replay/player.go` | CREATE | Reads JSONL, injects FakeClock, drives IngestMarketData |
| `internal/shared/replay/sequencer.go` | CREATE | ReplaySequencer: returns seq from fixture instead of generating |
| `internal/shared/replay/fixture.go` | CREATE | JSONL reader/writer helpers |
| `internal/shared/replay/recorder_test.go` | CREATE | Unit tests for recording |
| `internal/shared/replay/player_test.go` | CREATE | Unit tests for replay determinism |
| `internal/shared/replay/golden_test.go` | CREATE | Golden test framework with update flag |
| `internal/core/marketdata/app/ingest_golden_test.go` | CREATE | Golden test for ingest pipeline |
| `internal/core/aggregation/app/agg_golden_test.go` | CREATE | Golden test for aggregation pipeline |
| `cmd/consumer/main.go` | ALTER | Add -record and -replay flags |
| `testdata/fixtures/` | CREATE | Directory for fixture files |
| `testdata/golden/` | CREATE | Directory for golden output files |
| `Makefile` | ALTER | Add `record-fixture` and `golden-update` targets |

## 5. API Design

### Recorder

```go
package replay

// Recorder wraps an EventPublisher, intercepting every Publish call
// to write the envelope to a JSONL fixture file.
type Recorder struct {
    inner   ports.EventPublisher
    writer  *FixtureWriter
}

func NewRecorder(inner ports.EventPublisher, path string) (*Recorder, error)

// Publish writes envelope to fixture file, then forwards to inner publisher.
func (r *Recorder) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem

// Close flushes and closes the fixture file.
func (r *Recorder) Close() error

// Count returns number of envelopes recorded.
func (r *Recorder) Count() int64
```

### Player

```go
// Player replays a fixture file through an IngestMarketData usecase.
type Player struct {
    fixtures []FixtureLine
    clock    *clock.FakeClock
    seq      *ReplaySequencer
    output   []envelope.Envelope
}

type ReplayConfig struct {
    FixturePath string
    MaxEvents   int // 0 = all
}

func NewPlayer(cfg ReplayConfig) (*Player, error)

// Play executes the replay, feeding each fixture envelope through the pipeline.
// Returns all output envelopes produced.
func (p *Player) Play(ingest *app.IngestMarketData) ([]envelope.Envelope, *problem.Problem)

// OutputMatches compares output against a golden file.
// Returns nil if match, or a Problem describing the first difference.
func (p *Player) OutputMatches(goldenPath string) *problem.Problem
```

### ReplaySequencer

```go
// ReplaySequencer implements ports.Sequencer.
// Instead of generating new sequence numbers, it returns the seq
// from the fixture envelope being replayed.
type ReplaySequencer struct {
    currentSeq map[string]int64 // key: venue:instrument
}

func NewReplaySequencer() *ReplaySequencer

// SetSeq sets the next sequence number for a stream (called by Player before each event).
func (s *ReplaySequencer) SetSeq(venue, instrument string, seq int64)

// Next returns the pre-set sequence number for the stream.
func (s *ReplaySequencer) Next(venue, instrument string) int64
```

### Fixture Format

JSON-lines (`.jsonl`), one envelope per line:
```json
{"type":"marketdata.trade","version":1,"venue":"BINANCE","instrument":"BTCUSDT","ts_exchange":1710000001000,"ts_ingest":1710000002000,"seq":1,"idempotency_key":"a1b2c3...","meta":{},"payload":"eyJwcmljZSI6..."}
{"type":"marketdata.bookdelta","version":1,"venue":"BINANCE","instrument":"BTCUSDT","ts_exchange":1710000001500,"ts_ingest":1710000002500,"seq":2,"idempotency_key":"d4e5f6...","meta":{},"payload":"eyJiaWRzIjo..."}
```

Properties:
- Streamable (no need to load entire file into memory)
- Append-only (partial writes don't corrupt previous lines)
- Human-readable (debuggable with `jq`, `wc -l`, `head`)
- Each line is a complete, self-contained envelope

### FixtureWriter / FixtureReader

```go
// FixtureWriter appends envelopes to a JSONL file.
type FixtureWriter struct {
    file *os.File
    enc  *json.Encoder
    mu   sync.Mutex
}

func NewFixtureWriter(path string) (*FixtureWriter, error)
func (w *FixtureWriter) Write(env envelope.Envelope) error
func (w *FixtureWriter) Close() error

// FixtureReader reads envelopes from a JSONL file one at a time.
type FixtureReader struct {
    scanner *bufio.Scanner
    dec     *json.Decoder
    lineNum int
}

func NewFixtureReader(path string) (*FixtureReader, error)
func (r *FixtureReader) Next() (FixtureLine, bool, error)
func (r *FixtureReader) Close() error

type FixtureLine struct {
    Envelope envelope.Envelope
    LineNum  int
}
```

## 6. Replay Flow

```
1. Player loads fixture file via FixtureReader
2. For each fixture line:
   a. Set FakeClock to fixture.TsIngest
   b. Set ReplaySequencer to fixture.Seq for (venue, instrument)
   c. Convert fixture envelope back to IngestRequest (reverse of BuildEnvelope)
   d. Call IngestMarketData.Execute(request)
   e. Capture output envelope from spy EventPublisher
3. After all lines:
   a. Serialize all output envelopes to JSONL
   b. Compare against golden file (if provided)
   c. Report pass/fail with first difference location
```

### Reverse Mapping (Envelope → IngestRequest)

```go
func EnvelopeToIngestRequest(env envelope.Envelope) (app.IngestRequest, *problem.Problem) {
    // Decode payload based on env.Type + env.Version
    // Reconstruct IngestRequest fields from envelope metadata
    // This is the inverse of InstrumentStream.BuildEnvelope
}
```

## 7. Golden Test Pattern

```go
// internal/core/marketdata/app/ingest_golden_test.go

var updateGolden = flag.Bool("update-golden", false, "update golden files")

func TestGoldenIngestReplay(t *testing.T) {
    player, _ := replay.NewPlayer(replay.ReplayConfig{
        FixturePath: "testdata/fixtures/binance-1000.jsonl",
    })

    clk := clock.NewFakeClock(time.Time{})
    seq := replay.NewReplaySequencer()
    spy := &SpyPublisher{}

    ingest := app.NewIngestMarketData(clk, seq, spy, app.IngestConfig{
        MaxStreams: 10000,
        StreamTTL: time.Hour,
    })

    output, err := player.Play(ingest)
    require.Nil(t, err)

    goldenPath := "testdata/golden/binance-1000-output.jsonl"
    if *updateGolden {
        writeGolden(t, goldenPath, output)
        return
    }

    prob := player.OutputMatches(goldenPath)
    require.Nil(t, prob, "golden mismatch: %v", prob)
}
```

Usage:
```bash
# First time: generate golden files
go test -run TestGoldenIngestReplay -update-golden ./internal/core/marketdata/app/

# CI: validate against golden files
go test -run TestGoldenIngestReplay ./internal/core/marketdata/app/
```

## 8. CI Integration

### INV-R1 Grep Check

```bash
# Add to CI pipeline:
if grep -rn "time\.Now()" internal/core/; then
    echo "FAIL: time.Now() found in internal/core/ — use clock.Clock port instead"
    exit 1
fi
```

### Makefile Targets

```makefile
.PHONY: record-fixture golden-update golden-check

record-fixture:
	go run cmd/consumer/main.go -record=testdata/fixtures/binance-1000.jsonl \
	    -tickers=BTCUSDT,ETHUSDT -duration=5m

golden-update:
	go test -run TestGolden -update-golden ./internal/core/...

golden-check:
	go test -run TestGolden ./internal/core/...
```

## 9. Replay Modes

| Mode | Sequencer | Clock | Flag | Use Case |
|------|-----------|-------|------|----------|
| **Full replay** | ReplaySequencer (fixture seq) | FakeClock (fixture TsIngest) | `-replay=file.jsonl` | Golden tests, system rebuild |
| **Live + record** | Live sequencer | SystemClock | `-record=file.jsonl` | Fixture capture |
| **Catchup window** | Live sequencer (continue from last) | SystemClock | (future: JetStream replay) | Recovery after downtime |

## 10. Test Plan

| Type | Test | Pass Criteria |
|------|------|---------------|
| Unit | FixtureWriter writes valid JSONL | Each line parses as envelope |
| Unit | FixtureReader reads back exactly what was written | Field-by-field equality |
| Unit | ReplaySequencer returns pre-set seq values | Seq matches fixture |
| Unit | Recorder forwards to inner publisher after writing | Inner receives all envelopes |
| Unit | Player with 10-envelope fixture produces 10 outputs | Count match |
| Integration | Record 100 envelopes, replay, compare output | Byte-identical output |
| Golden | `TestGoldenIngestReplay` with 1000-envelope fixture | Matches golden file |
| Golden | `TestGoldenAggReplay` with bookdelta fixture | Matches golden file |
| CI | `grep time.Now internal/core/` | 0 matches |
| Determinism | Replay same fixture 3 times | All 3 outputs identical |

## 11. Acceptance Criteria

- [ ] `Recorder` captures envelopes to JSONL without corruption (verified by round-trip read)
- [ ] `Player` replays fixture through IngestMarketData and produces deterministic output
- [ ] `FakeClock` in replay uses `TsIngest` from fixture (not wall clock)
- [ ] `ReplaySequencer` returns fixture seq values (not new assignments)
- [ ] Golden test fails if domain logic changes output (intentional regression detection)
- [ ] `go test -run TestGolden -update-golden` regenerates golden files successfully
- [ ] At least one 1000-envelope fixture recorded from live Binance stream
- [ ] Replaying same fixture 3 times produces identical output (determinism proof)
- [ ] `grep -rn "time.Now()" internal/core/` returns 0 matches (CI enforced)
- [ ] `-record` flag in `cmd/consumer` captures to file during live operation
- [ ] `-replay` flag in `cmd/consumer` processes fixture file and exits
- [ ] `go test -race ./...` green across all modules
