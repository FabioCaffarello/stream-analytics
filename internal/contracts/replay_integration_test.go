package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/contracts"
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

func TestFixtureRoundtripJSON(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "roundtrip-json.jsonl")
	expected := writeJSONFixtureRecords(t, path, 10)
	verifyJSONFixtureRecords(t, path, expected)
}

func TestFixtureRoundtripProtoBytes(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "roundtrip-proto.jsonl")
	expected := writeProtoFixtureRecords(t, path, 10)
	verifyProtoFixtureRecords(t, path, expected)
}

func TestFixtureChecksumMismatch(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "checksum-mismatch.jsonl")
	w := newTestWriter(t, path)

	payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, marketdomain.TradeTickV1{
		Price:     42,
		Size:      1,
		Side:      "buy",
		TradeID:   "checksum-1",
		Timestamp: 1_710_000_000_001,
	})
	if p != nil {
		t.Fatalf("EncodePayload: %v", p)
	}

	env := envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsExchange:     1_710_000_000_010,
		TsIngest:       1_710_000_000_020,
		Seq:            1,
		IdempotencyKey: "idem-checksum",
		ContentType:    envelope.ContentTypeJSON,
		Payload:        payload,
	}
	if p := w.Append(env); p != nil {
		t.Fatalf("Append: %v", p)
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}

	corruptFixtureChecksum(t, path)

	r := newTestReader(t, path)
	t.Cleanup(func() {
		_ = r.Close()
	})

	_, _, p = r.Next()
	if p == nil {
		t.Fatal("expected checksum mismatch problem, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestFixtureUnknownContentTypeFailsDeterministically(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "unknown-content-type.jsonl")
	w := newTestWriter(t, path)
	env := buildJSONFixtureEnvelope(t, 0)
	if p := w.Append(env); p != nil {
		t.Fatalf("Append: %v", p)
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}

	// #nosec G304 -- path is test-local and created via t.TempDir.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	mutated := bytes.Replace(raw, []byte(`"content_type":"application/json"`), []byte(`"content_type":"application/unknown"`), 1)
	// #nosec G304 -- path is test-local and created via t.TempDir.
	if err := os.WriteFile(path, mutated, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := newTestReader(t, path)
	defer func() { _ = r.Close() }()
	_, _, p := r.Next()
	if p == nil {
		t.Fatal("expected unknown content_type error")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestFixtureReaderInvalidLineFailsDeterministically(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "invalid-line.jsonl")
	w := newTestWriter(t, path)
	if p := w.Append(buildJSONFixtureEnvelope(t, 0)); p != nil {
		t.Fatalf("Append: %v", p)
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}

	// #nosec G304 -- path is test-local and created via t.TempDir.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	raw = append(raw, []byte("{not-json}\n")...)
	// #nosec G304 -- path is test-local and created via t.TempDir.
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	assertReadDeterministicInvalidLine := func() {
		r := newTestReader(t, path)
		defer func() { _ = r.Close() }()

		if _, ok, p := r.Next(); !ok || p != nil {
			t.Fatalf("first line should be valid: ok=%v problem=%v", ok, p)
		}
		_, _, p := r.Next()
		if p == nil {
			t.Fatal("expected invalid-line problem")
		}
		if p.Code != problem.ValidationFailed {
			t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
		}
		if got := p.Details["line"]; got != "2" {
			t.Fatalf("line detail=%v want=2", got)
		}
	}

	assertReadDeterministicInvalidLine()
	assertReadDeterministicInvalidLine()
}

func TestDeterministicEncodingStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deterministic.jsonl")
	w := newTestWriter(t, path)

	for i := 0; i < 100; i++ {
		env := envelope.Envelope{
			Type:           "marketdata.trade",
			Version:        1,
			Venue:          "binance",
			Instrument:     "BTC-USDT",
			TsExchange:     1_710_000_700_000,
			TsIngest:       1_710_000_800_000,
			Seq:            77,
			IdempotencyKey: "idem-deterministic",
			ContentType:    envelope.ContentTypeJSON,
			Meta:           mapMetaVariant(i%2 == 0),
			Payload:        []byte(`{"z":1,"a":{"k2":2,"k1":1},"arr":[{"y":2,"x":1}]}`),
		}
		if p := w.Append(env); p != nil {
			t.Fatalf("Append[%d]: %v", i, p)
		}
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}

	// #nosec G304 -- path is test-local and created via t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 100 {
		t.Fatalf("line count=%d want=100", len(lines))
	}

	first := lines[0]
	for i := 1; i < len(lines); i++ {
		if !bytes.Equal(lines[i], first) {
			t.Fatalf("line[%d] differs from line[0]", i)
		}
	}
}

func writeJSONFixtureRecords(t *testing.T, path string, n int) []envelope.Envelope {
	t.Helper()
	w := newTestWriter(t, path)
	defer func() {
		_ = w.Close()
	}()

	expected := make([]envelope.Envelope, 0, n)
	for i := 0; i < n; i++ {
		env := buildJSONFixtureEnvelope(t, i)
		if p := w.Append(env); p != nil {
			t.Fatalf("Append[%d]: %v", i, p)
		}
		expected = append(expected, env)
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}
	return expected
}

func writeProtoFixtureRecords(t *testing.T, path string, n int) []envelope.Envelope {
	t.Helper()
	w := newTestWriter(t, path)
	defer func() {
		_ = w.Close()
	}()

	expected := make([]envelope.Envelope, 0, n)
	for i := 0; i < n; i++ {
		env := buildProtoFixtureEnvelope(t, i)
		if p := w.Append(env); p != nil {
			t.Fatalf("Append[%d]: %v", i, p)
		}
		expected = append(expected, env)
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}
	return expected
}

func verifyJSONFixtureRecords(t *testing.T, path string, expected []envelope.Envelope) {
	t.Helper()
	r := newTestReader(t, path)
	defer func() {
		_ = r.Close()
	}()

	for i := range expected {
		rec := mustNextRecord(t, r, i)
		want := expected[i]
		assertJSONRecord(t, i, rec, want)
	}
	assertReaderEOF(t, r)
}

func verifyProtoFixtureRecords(t *testing.T, path string, expected []envelope.Envelope) {
	t.Helper()
	r := newTestReader(t, path)
	defer func() {
		_ = r.Close()
	}()

	for i := range expected {
		rec := mustNextRecord(t, r, i)
		want := expected[i]
		assertProtoRecord(t, i, rec, want)
	}
	assertReaderEOF(t, r)
}

func assertJSONRecord(t *testing.T, idx int, rec replay.FixtureRecord, want envelope.Envelope) {
	t.Helper()
	if rec.Subject != envelope.SubjectFromEnvelope(want) {
		t.Fatalf("subject[%d]=%q want=%q", idx, rec.Subject, envelope.SubjectFromEnvelope(want))
	}
	if rec.PayloadB64 != "" {
		t.Fatalf("payload_b64[%d] should be empty for json content", idx)
	}
	if len(rec.PayloadJSON) == 0 {
		t.Fatalf("payload_json[%d] is empty", idx)
	}
	if !bytes.Equal(rec.Envelope.Payload, rec.PayloadJSON) {
		t.Fatalf("payload bytes[%d] must match payload_json", idx)
	}

	gotAny, p := codec.DecodePayload(rec.Envelope.Type, rec.Envelope.Version, rec.Envelope.ContentType, rec.Envelope.Payload)
	if p != nil {
		t.Fatalf("DecodePayload(got)[%d]: %v", idx, p)
	}
	wantAny, p := codec.DecodePayload(want.Type, want.Version, want.ContentType, want.Payload)
	if p != nil {
		t.Fatalf("DecodePayload(want)[%d]: %v", idx, p)
	}
	if !reflect.DeepEqual(gotAny, wantAny) {
		t.Fatalf("semantic payload mismatch[%d]: got=%#v want=%#v", idx, gotAny, wantAny)
	}
	assertEnvelopeCoreEqual(t, idx, rec.Envelope, want)
}

func assertProtoRecord(t *testing.T, idx int, rec replay.FixtureRecord, want envelope.Envelope) {
	t.Helper()
	if rec.Subject != envelope.SubjectFromEnvelope(want) {
		t.Fatalf("subject[%d]=%q want=%q", idx, rec.Subject, envelope.SubjectFromEnvelope(want))
	}
	if len(rec.PayloadJSON) != 0 {
		t.Fatalf("payload_json[%d] should be empty for proto content", idx)
	}
	if rec.PayloadB64 == "" {
		t.Fatalf("payload_b64[%d] is empty", idx)
	}
	if !bytes.Equal(rec.Envelope.Payload, want.Payload) {
		t.Fatalf("proto payload bytes mismatch[%d]", idx)
	}
	assertEnvelopeCoreEqual(t, idx, rec.Envelope, want)
}

func newTestWriter(t *testing.T, path string) *replay.Writer {
	t.Helper()
	w, p := replay.NewWriter(path)
	if p != nil {
		t.Fatalf("NewWriter: %v", p)
	}
	return w
}

func newTestReader(t *testing.T, path string) *replay.Reader {
	t.Helper()
	r, p := replay.NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	return r
}

func mustNextRecord(t *testing.T, r *replay.Reader, idx int) replay.FixtureRecord {
	t.Helper()
	rec, ok, p := r.Next()
	if p != nil {
		t.Fatalf("Next[%d]: %v", idx, p)
	}
	if !ok {
		t.Fatalf("Next[%d]: unexpected EOF", idx)
	}
	return rec
}

func assertReaderEOF(t *testing.T, r *replay.Reader) {
	t.Helper()
	if _, ok, p := r.Next(); p != nil || ok {
		t.Fatalf("expected EOF: ok=%v problem=%v", ok, p)
	}
}

func buildJSONFixtureEnvelope(t *testing.T, i int) envelope.Envelope {
	t.Helper()
	payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, marketdomain.TradeTickV1{
		Price:     1000.5 + float64(i),
		Size:      0.1 + float64(i)/10,
		Side:      "buy",
		TradeID:   "trade-json-" + strings.Repeat("x", i+1),
		Timestamp: 1_710_000_000_000 + int64(i),
	})
	if p != nil {
		t.Fatalf("EncodePayload[%d]: %v", i, p)
	}
	return envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsExchange:     1_710_000_100_000 + int64(i),
		TsIngest:       1_710_000_200_000 + int64(i),
		Seq:            int64(i + 1),
		IdempotencyKey: "idem-json-" + strings.Repeat("a", i+1),
		ContentType:    envelope.ContentTypeJSON,
		Meta:           mapMetaVariant(i%2 == 0),
		Payload:        payload,
	}
}

func buildProtoFixtureEnvelope(t *testing.T, i int) envelope.Envelope {
	t.Helper()
	payload, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeProto, marketdomain.BookDeltaV1{
		Bids:      []marketdomain.PriceLevel{{Price: 50000 + float64(i), Size: 1.0 + float64(i)/10}},
		Asks:      []marketdomain.PriceLevel{{Price: 50001 + float64(i), Size: 2.0 + float64(i)/10}},
		FirstID:   int64(100 + i),
		FinalID:   int64(200 + i),
		PrevFinal: int64(99 + i),
		Timestamp: 1_710_000_300_000 + int64(i),
	})
	if p != nil {
		t.Fatalf("EncodePayload[%d]: %v", i, p)
	}
	return envelope.Envelope{
		Type:           "marketdata.bookdelta",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsExchange:     1_710_000_400_000 + int64(i),
		TsIngest:       1_710_000_500_000 + int64(i),
		Seq:            int64(i + 1),
		IdempotencyKey: "idem-proto-" + strings.Repeat("b", i+1),
		ContentType:    envelope.ContentTypeProto,
		Meta:           mapMetaVariant(i%2 == 0),
		Payload:        payload,
	}
}

func corruptFixtureChecksum(t *testing.T, path string) {
	t.Helper()

	// #nosec G304 -- path is test-local and created via t.TempDir.
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	line := strings.TrimSpace(string(rawBytes))

	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("json.Unmarshal line: %v", err)
	}
	sha, ok := m["sha256"].(string)
	if !ok || sha == "" {
		t.Fatalf("missing sha256 in fixture line")
	}
	m["sha256"] = mutateSHA(sha)

	corrupted, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal corrupted line: %v", err)
	}
	// #nosec G304 -- path is test-local and created via t.TempDir.
	if err := os.WriteFile(path, append(corrupted, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile corrupted line: %v", err)
	}
}

func mutateSHA(sha string) string {
	if strings.HasPrefix(sha, "0") {
		return "1" + sha[1:]
	}
	return "0" + sha[1:]
}

func mustBootstrapPayloadRegistry(t *testing.T) {
	t.Helper()
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}
}

func mapMetaVariant(normalOrder bool) map[string]string {
	if normalOrder {
		return map[string]string{
			"source":   "fixture",
			"parser":   "v1",
			"exchange": "binance",
		}
	}
	m := make(map[string]string, 3)
	m["exchange"] = "binance"
	m["parser"] = "v1"
	m["source"] = "fixture"
	return m
}

func assertEnvelopeCoreEqual(t *testing.T, idx int, got, want envelope.Envelope) {
	t.Helper()

	if got.Type != want.Type {
		t.Fatalf("type[%d]=%q want=%q", idx, got.Type, want.Type)
	}
	if got.Version != want.Version {
		t.Fatalf("version[%d]=%d want=%d", idx, got.Version, want.Version)
	}
	if got.Venue != want.Venue {
		t.Fatalf("venue[%d]=%q want=%q", idx, got.Venue, want.Venue)
	}
	if got.Instrument != want.Instrument {
		t.Fatalf("instrument[%d]=%q want=%q", idx, got.Instrument, want.Instrument)
	}
	if got.TsExchange != want.TsExchange {
		t.Fatalf("ts_exchange[%d]=%d want=%d", idx, got.TsExchange, want.TsExchange)
	}
	if got.TsIngest != want.TsIngest {
		t.Fatalf("ts_ingest[%d]=%d want=%d", idx, got.TsIngest, want.TsIngest)
	}
	if got.Seq != want.Seq {
		t.Fatalf("seq[%d]=%d want=%d", idx, got.Seq, want.Seq)
	}
	if got.IdempotencyKey != want.IdempotencyKey {
		t.Fatalf("idempotency_key[%d]=%q want=%q", idx, got.IdempotencyKey, want.IdempotencyKey)
	}
	if got.ContentType != want.ContentType {
		t.Fatalf("content_type[%d]=%q want=%q", idx, got.ContentType, want.ContentType)
	}
	if !reflect.DeepEqual(got.Meta, want.Meta) {
		t.Fatalf("meta[%d]=%v want=%v", idx, got.Meta, want.Meta)
	}
}
