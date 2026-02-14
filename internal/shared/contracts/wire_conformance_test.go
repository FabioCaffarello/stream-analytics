package contracts_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestWireConformance_SubjectPayloadCodecDecode_TradeProto(t *testing.T) {
	reg := newConformanceRegistry(t)
	in := marketdomain.TradeTickV1{
		Price:     65000.5,
		Size:      0.25,
		Side:      "buy",
		TradeID:   "t-100",
		Timestamp: 1_710_000_000_100,
	}

	subject := envelope.SubjectFromEnvelope(envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
	})

	raw := mustEncode(t, reg, codec.SchemaKey{Type: "marketdata.trade", Version: 1, Format: codec.FormatProto}, in)
	outAny, p := decodeBySubject(reg, subject, envelope.ContentTypeProto, raw)
	if p != nil {
		t.Fatalf("decodeBySubject: %v", p)
	}
	out, ok := outAny.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", outAny, marketdomain.TradeTickV1{})
	}
	if out != in {
		t.Fatalf("decoded mismatch\ngot=%+v\nwant=%+v", out, in)
	}
}

func TestWireConformance_SubjectPayloadCodecDecode_VPVRSnapshotProto(t *testing.T) {
	reg := newConformanceRegistry(t)
	in := testVPVRSnapshot()

	subject := envelope.SubjectFromEnvelope(envelope.Envelope{
		Type:       insightsdomain.VolumeProfileSnapshotType,
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
	})

	raw := mustEncode(t, reg, codec.SchemaKey{Type: insightsdomain.VolumeProfileSnapshotType, Version: 1, Format: codec.FormatProto}, in)
	outAny, p := decodeBySubject(reg, subject, envelope.ContentTypeProto, raw)
	if p != nil {
		t.Fatalf("decodeBySubject: %v", p)
	}
	out, ok := outAny.(insightsdomain.VolumeProfileSnapshotV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", outAny, insightsdomain.VolumeProfileSnapshotV1{})
	}
	if len(out.Buckets) != len(in.Buckets) || out.WindowStartTs != in.WindowStartTs || out.WindowEndTs != in.WindowEndTs {
		t.Fatalf("decoded mismatch\ngot=%+v\nwant=%+v", out, in)
	}
}

func TestWireConformance_RejectsPayloadMismatchForSubjectProto(t *testing.T) {
	reg := newConformanceRegistry(t)
	wrong := marketdomain.BookDeltaV1{
		Bids:      []marketdomain.PriceLevel{{Price: 65000, Size: 1.0}},
		Asks:      []marketdomain.PriceLevel{{Price: 65001, Size: 1.5}},
		FirstID:   10,
		FinalID:   11,
		PrevFinal: 9,
		Timestamp: 1_710_000_000_100,
	}
	raw := mustEncode(t, reg, codec.SchemaKey{Type: "marketdata.bookdelta", Version: 1, Format: codec.FormatProto}, wrong)

	subject := envelope.SubjectFromEnvelope(envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
	})

	_, p := decodeBySubject(reg, subject, envelope.ContentTypeProto, raw)
	if p == nil {
		t.Fatal("expected validation problem for payload/subject mismatch")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code=%q want=%q", p.Code, problem.ValidationFailed)
	}
}

func newConformanceRegistry(t *testing.T) *codec.Registry {
	t.Helper()
	reg := codec.NewRegistry()
	if p := contracts.RegisterMarketDataPayloadV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataPayloadV1: %v", p)
	}
	if p := contracts.RegisterInsightsPayloadV1WithOptions(reg, contracts.InsightsCodecOptions{
		EnableVolumeProfileSnapshotProto: true,
	}); p != nil {
		t.Fatalf("RegisterInsightsPayloadV1WithOptions: %v", p)
	}
	return reg
}

func mustEncode(t *testing.T, reg *codec.Registry, key codec.SchemaKey, payload any) []byte {
	t.Helper()
	enc, ok := reg.Encoder(key)
	if !ok {
		t.Fatalf("missing encoder for key %+v", key)
	}
	raw, p := enc.Encode(payload)
	if p != nil {
		t.Fatalf("encode %+v: %v", key, p)
	}
	return raw
}

func decodeBySubject(reg *codec.Registry, subject string, contentType string, payload []byte) (any, *problem.Problem) {
	eventType, version, p := parseEventTypeAndVersion(subject)
	if p != nil {
		return nil, p
	}
	format, p := contentTypeToFormat(contentType)
	if p != nil {
		return nil, p
	}
	key := codec.SchemaKey{Type: eventType, Version: version, Format: format}
	dec, ok := reg.Decoder(key)
	if !ok {
		return nil, problem.Newf(problem.ValidationFailed, "no decoder for subject type=%q version=%d format=%q", eventType, version, format)
	}
	decoded, p := dec.Decode(payload)
	if p != nil {
		return nil, p
	}

	// Explicit wire-level conformance: payload must be canonical for the subject codec.
	enc, ok := reg.Encoder(key)
	if !ok {
		return nil, problem.Newf(problem.ValidationFailed, "no encoder for subject type=%q version=%d format=%q", eventType, version, format)
	}
	canonical, p := enc.Encode(decoded)
	if p != nil {
		return nil, p
	}
	if !bytes.Equal(payload, canonical) {
		return nil, problem.Newf(problem.ValidationFailed, "payload does not conform to subject codec type=%q version=%d", eventType, version)
	}
	return decoded, nil
}

func parseEventTypeAndVersion(subject string) (string, int32, *problem.Problem) {
	parts := strings.Split(strings.TrimSpace(subject), ".")
	if len(parts) < 4 {
		return "", 0, problem.Newf(problem.ValidationFailed, "invalid subject format %q", subject)
	}
	versionToken := parts[len(parts)-3]
	if !strings.HasPrefix(versionToken, "v") || len(versionToken) < 2 {
		return "", 0, problem.Newf(problem.ValidationFailed, "invalid subject version token %q", versionToken)
	}
	version, err := strconv.ParseInt(strings.TrimPrefix(versionToken, "v"), 10, 32)
	if err != nil || version < 1 {
		return "", 0, problem.Newf(problem.ValidationFailed, "invalid subject version token %q", versionToken)
	}
	eventType := strings.Join(parts[:len(parts)-3], ".")
	if strings.TrimSpace(eventType) == "" {
		return "", 0, problem.New(problem.ValidationFailed, "subject event_type must not be empty")
	}
	return eventType, int32(version), nil
}

func contentTypeToFormat(contentType string) (codec.Format, *problem.Problem) {
	switch strings.TrimSpace(contentType) {
	case envelope.ContentTypeJSON:
		return codec.FormatJSON, nil
	case envelope.ContentTypeProto:
		return codec.FormatProto, nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "unsupported content_type %q", contentType)
	}
}
