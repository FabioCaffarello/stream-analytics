//go:build integration

package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/contracts"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestWireConformance_SubjectPayloadCodecDecode_TradeProto(t *testing.T) {
	runMarketDataSubjectProtoRoundtrip(t, "marketdata.trade", marketdomain.TradeTickV1{
		Price:     65000.5,
		Size:      0.25,
		Side:      "buy",
		TradeID:   "t-100",
		Timestamp: 1_710_000_000_100,
	}, validateTradeTick)
}

func TestWireConformance_SubjectPayloadCodecDecode_BookDeltaProto(t *testing.T) {
	runMarketDataSubjectProtoRoundtrip(t, "marketdata.bookdelta", marketdomain.BookDeltaV1{
		Bids:      []marketdomain.PriceLevel{{Price: 65000.1, Size: 1.2}},
		Asks:      []marketdomain.PriceLevel{{Price: 65001.3, Size: 0.8}},
		FirstID:   100,
		FinalID:   101,
		PrevFinal: 99,
		Timestamp: 1_710_000_000_200,
	}, validateBookDelta)
}

func TestWireConformance_SubjectPayloadCodecDecode_MarkPriceProto(t *testing.T) {
	runMarketDataSubjectProtoRoundtrip(t, "marketdata.markprice", marketdomain.MarkPriceTickV1{
		MarkPrice:   64999.7,
		IndexPrice:  65000.0,
		FundingRate: 0.0001,
		Timestamp:   1_710_000_000_300,
	}, validateMarkPrice)
}

func TestWireConformance_SubjectPayloadCodecDecode_OpenInterestProto(t *testing.T) {
	runMarketDataSubjectProtoRoundtrip(t, "marketdata.open_interest", marketdomain.OpenInterestTickV1{
		OpenInterest: 1_250_000.5,
		Timestamp:    1_710_000_000_350,
	}, validateOpenInterest)
}

func runMarketDataSubjectProtoRoundtrip(t *testing.T, eventType string, payload any, validate func(t *testing.T, got any, want any)) {
	t.Helper()
	reg := newConformanceRegistry(t)
	subject := envelope.SubjectFromEnvelope(envelope.Envelope{
		Type:       eventType,
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
	})
	raw := mustEncode(t, reg, codec.SchemaKey{Type: eventType, Version: 1, Format: codec.FormatProto}, payload)
	outAny, p := decodeBySubject(reg, subject, envelope.ContentTypeProto, raw)
	if p != nil {
		t.Fatalf("decodeBySubject: %v", p)
	}
	validate(t, outAny, payload)
}

func validateTradeTick(t *testing.T, got any, want any) {
	t.Helper()
	out, ok := got.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", got, marketdomain.TradeTickV1{})
	}
	in := want.(marketdomain.TradeTickV1)
	if out != in {
		t.Fatalf("decoded mismatch\ngot=%+v\nwant=%+v", out, in)
	}
	if out.Side != "buy" && out.Side != "sell" {
		t.Fatalf("invalid trade side enum %q", out.Side)
	}
	if out.TradeID == "" || out.Timestamp <= 0 {
		t.Fatalf("missing required trade fields: %+v", out)
	}
}

func validateBookDelta(t *testing.T, got any, want any) {
	t.Helper()
	out, ok := got.(marketdomain.BookDeltaV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", got, marketdomain.BookDeltaV1{})
	}
	in := want.(marketdomain.BookDeltaV1)
	if len(out.Bids) != len(in.Bids) || len(out.Asks) != len(in.Asks) || out.FirstID != in.FirstID || out.FinalID != in.FinalID || out.Timestamp != in.Timestamp {
		t.Fatalf("decoded mismatch\ngot=%+v\nwant=%+v", out, in)
	}
	if out.FinalID < out.FirstID {
		t.Fatalf("invalid bookdelta ids first=%d final=%d", out.FirstID, out.FinalID)
	}
	if out.Timestamp <= 0 {
		t.Fatalf("missing required bookdelta timestamp: %+v", out)
	}
}

func validateMarkPrice(t *testing.T, got any, want any) {
	t.Helper()
	out, ok := got.(marketdomain.MarkPriceTickV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", got, marketdomain.MarkPriceTickV1{})
	}
	in := want.(marketdomain.MarkPriceTickV1)
	if out != in {
		t.Fatalf("decoded mismatch\ngot=%+v\nwant=%+v", out, in)
	}
	if out.Timestamp <= 0 {
		t.Fatalf("missing required markprice timestamp: %+v", out)
	}
}

func validateOpenInterest(t *testing.T, got any, want any) {
	t.Helper()
	out, ok := got.(marketdomain.OpenInterestTickV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", got, marketdomain.OpenInterestTickV1{})
	}
	in := want.(marketdomain.OpenInterestTickV1)
	if out != in {
		t.Fatalf("decoded mismatch\ngot=%+v\nwant=%+v", out, in)
	}
	if out.Timestamp <= 0 {
		t.Fatalf("missing required open_interest timestamp: %+v", out)
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

func TestWireConformance_RegistrySchemasRequireProtoDecoderForCoreMarketData(t *testing.T) {
	t.Parallel()

	reg := newConformanceRegistry(t)
	type schema struct {
		Type    string `json:"type"`
		Version int32  `json:"version"`
		Status  string `json:"status"`
	}
	var parsed struct {
		Schemas []schema `json:"schemas"`
	}

	path := filepath.Join(findRepoRootFromWD(t), "proto", "registry.json")
	// #nosec G304 -- path is repository-owned registry fixture.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	protoRequiredTypes := map[string]bool{
		"marketdata.trade":         true,
		"marketdata.bookdelta":     true,
		"marketdata.markprice":     true,
		"marketdata.open_interest": true,
		"aggregation.candle":       true,
		"aggregation.stats":        true,
		"aggregation.tape":         true,
		"aggregation.oi":           true,
		"aggregation.cvd":          true,
		"aggregation.delta_volume": true,
		"aggregation.bar_stats":    true,
	}
	for _, sch := range parsed.Schemas {
		if sch.Status != "stable" && sch.Status != "draft" {
			continue
		}
		if !protoRequiredTypes[sch.Type] {
			continue
		}
		key := codec.SchemaKey{Type: sch.Type, Version: sch.Version, Format: codec.FormatProto}
		if _, ok := reg.Decoder(key); !ok {
			t.Fatalf("missing proto decoder for registry schema type=%q version=%d", sch.Type, sch.Version)
		}
	}
}

func findRepoRootFromWD(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("unable to locate repository root from %s", wd)
		}
		dir = parent
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
		EnableHeatmapSnapshotProto:       true,
	}); p != nil {
		t.Fatalf("RegisterInsightsPayloadV1WithOptions: %v", p)
	}
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
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
