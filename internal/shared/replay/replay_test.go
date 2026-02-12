package replay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestFixtureRoundtripJSON(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "roundtrip-json.jsonl")
	w, p := NewWriter(path)
	if p != nil {
		t.Fatalf("NewWriter: %v", p)
	}
	t.Cleanup(func() {
		_ = w.Close()
	})

	expected := make([]envelope.Envelope, 0, 10)
	for i := 0; i < 10; i++ {
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

		env := envelope.Envelope{
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
		if p := w.Append(env); p != nil {
			t.Fatalf("Append[%d]: %v", i, p)
		}
		expected = append(expected, env)
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	t.Cleanup(func() {
		_ = r.Close()
	})

	for i := 0; i < len(expected); i++ {
		rec, ok, p := r.Next()
		if p != nil {
			t.Fatalf("Next[%d]: %v", i, p)
		}
		if !ok {
			t.Fatalf("Next[%d]: unexpected EOF", i)
		}
		want := expected[i]

		if rec.Subject != envelope.SubjectFromEnvelope(want) {
			t.Fatalf("subject[%d]=%q want=%q", i, rec.Subject, envelope.SubjectFromEnvelope(want))
		}
		if rec.PayloadB64 != "" {
			t.Fatalf("payload_b64[%d] should be empty for json content", i)
		}
		if len(rec.PayloadJSON) == 0 {
			t.Fatalf("payload_json[%d] is empty", i)
		}
		if !bytes.Equal(rec.Envelope.Payload, rec.PayloadJSON) {
			t.Fatalf("payload bytes[%d] must match payload_json", i)
		}

		gotAny, p := codec.DecodePayload(rec.Envelope.Type, rec.Envelope.Version, rec.Envelope.ContentType, rec.Envelope.Payload)
		if p != nil {
			t.Fatalf("DecodePayload(got)[%d]: %v", i, p)
		}
		wantAny, p := codec.DecodePayload(want.Type, want.Version, want.ContentType, want.Payload)
		if p != nil {
			t.Fatalf("DecodePayload(want)[%d]: %v", i, p)
		}
		if !reflect.DeepEqual(gotAny, wantAny) {
			t.Fatalf("semantic payload mismatch[%d]: got=%#v want=%#v", i, gotAny, wantAny)
		}

		assertEnvelopeCoreEqual(t, i, rec.Envelope, want)
	}

	if _, ok, p := r.Next(); p != nil || ok {
		t.Fatalf("expected EOF: ok=%v problem=%v", ok, p)
	}
}

func TestFixtureRoundtripProtoBytes(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "roundtrip-proto.jsonl")
	w, p := NewWriter(path)
	if p != nil {
		t.Fatalf("NewWriter: %v", p)
	}
	t.Cleanup(func() {
		_ = w.Close()
	})

	expected := make([]envelope.Envelope, 0, 10)
	for i := 0; i < 10; i++ {
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

		env := envelope.Envelope{
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
		if p := w.Append(env); p != nil {
			t.Fatalf("Append[%d]: %v", i, p)
		}
		expected = append(expected, env)
	}
	if p := w.Close(); p != nil {
		t.Fatalf("Close writer: %v", p)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	t.Cleanup(func() {
		_ = r.Close()
	})

	for i := 0; i < len(expected); i++ {
		rec, ok, p := r.Next()
		if p != nil {
			t.Fatalf("Next[%d]: %v", i, p)
		}
		if !ok {
			t.Fatalf("Next[%d]: unexpected EOF", i)
		}
		want := expected[i]

		if rec.Subject != envelope.SubjectFromEnvelope(want) {
			t.Fatalf("subject[%d]=%q want=%q", i, rec.Subject, envelope.SubjectFromEnvelope(want))
		}
		if len(rec.PayloadJSON) != 0 {
			t.Fatalf("payload_json[%d] should be empty for proto content", i)
		}
		if rec.PayloadB64 == "" {
			t.Fatalf("payload_b64[%d] is empty", i)
		}
		if !bytes.Equal(rec.Envelope.Payload, want.Payload) {
			t.Fatalf("proto payload bytes mismatch[%d]", i)
		}

		assertEnvelopeCoreEqual(t, i, rec.Envelope, want)
	}

	if _, ok, p := r.Next(); p != nil || ok {
		t.Fatalf("expected EOF: ok=%v problem=%v", ok, p)
	}
}

func TestFixtureChecksumMismatch(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	path := filepath.Join(t.TempDir(), "checksum-mismatch.jsonl")
	w, p := NewWriter(path)
	if p != nil {
		t.Fatalf("NewWriter: %v", p)
	}

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
	if strings.HasPrefix(sha, "0") {
		m["sha256"] = "1" + sha[1:]
	} else {
		m["sha256"] = "0" + sha[1:]
	}

	corrupted, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal corrupted line: %v", err)
	}
	if err := os.WriteFile(path, append(corrupted, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile corrupted line: %v", err)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
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

func TestDeterministicEncodingStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deterministic.jsonl")
	w, p := NewWriter(path)
	if p != nil {
		t.Fatalf("NewWriter: %v", p)
	}

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
