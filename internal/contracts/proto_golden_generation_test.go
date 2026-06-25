package contracts_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/contracts"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	envelopev1 "github.com/market-raccoon/internal/shared/proto/gen/envelope/v1"
	"google.golang.org/protobuf/proto"
)

func TestProtoGoldenV1_DeterministicWireFixtures(t *testing.T) {
	t.Parallel()

	fixtures := protoGoldenV1Fixtures()
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		for _, fixture := range fixtures {
			writeProtoGoldenHex(t, fixture.path, fixture.msg)
		}
	}

	for _, fixture := range fixtures {
		want := readProtoGoldenHex(t, fixture.path)
		got := marshalProtoDeterministicHex(t, fixture.msg)
		if got != want {
			t.Fatalf("%s golden mismatch\ngot=%s\nwant=%s", fixture.path, got, want)
		}
	}
}

func TestProto_BackwardCompat_DecodesGoldenV1(t *testing.T) {
	t.Parallel()

	reg := codec.NewRegistry()
	if p := contracts.RegisterMarketDataPayloadV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataPayloadV1: %v", p)
	}
	if p := contracts.RegisterInsightsPayloadV1WithOptions(reg, contracts.InsightsCodecOptions{
		EnableVolumeProfileSnapshotProto: true,
	}); p != nil {
		t.Fatalf("RegisterInsightsPayloadV1WithOptions: %v", p)
	}

	for _, tc := range protoBackwardDecodeCases(reg) {
		raw := decodeProtoGoldenHex(t, tc.path)
		if err := tc.decode(raw); err != nil {
			t.Fatalf("failed to decode golden %s: %v", tc.path, err)
		}
	}
}

func protoBackwardDecodeCases(reg *codec.Registry) []struct {
	path   string
	decode func([]byte) error
} {
	return []struct {
		path   string
		decode func([]byte) error
	}{
		{
			path: filepath.Join("testdata", "golden", "marketdata_trade_proto_v1.hex"),
			decode: func(raw []byte) error {
				return decodeDomainType(reg, "marketdata.trade", raw, func(out any) bool {
					_, ok := out.(marketdomain.TradeTickV1)
					return ok
				}, "trade")
			},
		},
		{
			path: filepath.Join("testdata", "golden", "marketdata_bookdelta_proto_v1.hex"),
			decode: func(raw []byte) error {
				return decodeDomainType(reg, "marketdata.bookdelta", raw, func(out any) bool {
					_, ok := out.(marketdomain.BookDeltaV1)
					return ok
				}, "bookdelta")
			},
		},
		{
			path: filepath.Join("testdata", "golden", "marketdata_markprice_proto_v1.hex"),
			decode: func(raw []byte) error {
				return decodeDomainType(reg, "marketdata.markprice", raw, func(out any) bool {
					_, ok := out.(marketdomain.MarkPriceTickV1)
					return ok
				}, "markprice")
			},
		},
		{
			path: filepath.Join("testdata", "golden", "insights_volume_profile_snapshot_proto_v1.hex"),
			decode: func(raw []byte) error {
				return decodeDomainType(reg, insightsdomain.VolumeProfileSnapshotType, raw, func(out any) bool {
					_, ok := out.(insightsdomain.VolumeProfileSnapshotV1)
					return ok
				}, "vpvr")
			},
		},
		{
			path: filepath.Join("testdata", "golden", "envelope_proto_v1.hex"),
			decode: func(raw []byte) error {
				var out envelopev1.Envelope
				if err := proto.Unmarshal(raw, &out); err != nil {
					return err
				}
				if strings.TrimSpace(out.GetType()) == "" {
					return errString("decoded envelope has empty type")
				}
				return nil
			},
		},
	}
}

func decodeDomainType(reg *codec.Registry, eventType string, raw []byte, accept func(any) bool, label string) error {
	dec, ok := reg.Decoder(codec.SchemaKey{Type: eventType, Version: 1, Format: codec.FormatProto})
	if !ok {
		return errString("missing " + eventType + " proto decoder")
	}
	outAny, p := dec.Decode(raw)
	if p != nil {
		return errString(p.Error())
	}
	if !accept(outAny) {
		return errString("decoded " + label + " payload has unexpected type")
	}
	return nil
}

type protoGoldenFixture struct {
	path string
	msg  proto.Message
}

func protoGoldenV1Fixtures() []protoGoldenFixture {
	return []protoGoldenFixture{
		{
			path: filepath.Join("testdata", "golden", "marketdata_trade_proto_v1.hex"),
			msg: contracts.DomainToProtoTradeTickV1(marketdomain.TradeTickV1{
				Price:     65000.5,
				Size:      0.25,
				Side:      "buy",
				TradeID:   "trade-001",
				Timestamp: 1_710_000_000_100,
			}),
		},
		{
			path: filepath.Join("testdata", "golden", "marketdata_bookdelta_proto_v1.hex"),
			msg: contracts.DomainToProtoBookDeltaV1(marketdomain.BookDeltaV1{
				Bids: []marketdomain.PriceLevel{
					{Price: 65000.1, Size: 1.2},
					{Price: 64999.8, Size: 0.9},
				},
				Asks: []marketdomain.PriceLevel{
					{Price: 65000.7, Size: 1.1},
					{Price: 65001.2, Size: 0.7},
				},
				FirstID:   1200,
				FinalID:   1210,
				PrevFinal: 1199,
				Timestamp: 1_710_000_000_250,
			}),
		},
		{
			path: filepath.Join("testdata", "golden", "marketdata_markprice_proto_v1.hex"),
			msg: contracts.DomainToProtoMarkPriceTickV1(marketdomain.MarkPriceTickV1{
				MarkPrice:   64999.75,
				IndexPrice:  65000.00,
				FundingRate: 0.0001,
				Timestamp:   1_710_000_000_300,
			}),
		},
		{
			path: filepath.Join("testdata", "golden", "insights_volume_profile_snapshot_proto_v1.hex"),
			msg:  contracts.DomainToProtoVolumeProfileSnapshotV1(testVPVRSnapshot()),
		},
		{
			path: filepath.Join("testdata", "golden", "envelope_proto_v1.hex"),
			msg: &envelopev1.Envelope{
				Type:           "marketdata.trade",
				Version:        1,
				Venue:          "binance",
				Instrument:     "BTCUSDT",
				TsExchange:     1_710_000_000_050,
				TsIngest:       1_710_000_000_100,
				Seq:            1001,
				IdempotencyKey: "idem-marketdata-trade-1001",
				Meta: map[string]string{
					"producer": "consumer",
					"schema":   "v1",
				},
				Payload:     []byte{0x0a, 0x01, 0x01},
				ContentType: "application/protobuf",
			},
		},
	}
}

func marshalProtoDeterministicHex(t *testing.T, msg proto.Message) string {
	t.Helper()
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(msg)
	if err != nil {
		t.Fatalf("marshal deterministic proto %T: %v", msg, err)
	}
	return hex.EncodeToString(raw)
}

func readProtoGoldenHex(t *testing.T, path string) string {
	t.Helper()
	// #nosec G304 -- path points to repository-owned test fixture files.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read proto golden %s: %v", path, err)
	}
	return strings.TrimSpace(string(raw))
}

func decodeProtoGoldenHex(t *testing.T, path string) []byte {
	t.Helper()
	rawHex := readProtoGoldenHex(t, path)
	raw, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("decode hex golden %s: %v", path, err)
	}
	return raw
}

func writeProtoGoldenHex(t *testing.T, path string, msg proto.Message) {
	t.Helper()
	rawHex := marshalProtoDeterministicHex(t, msg)
	if err := os.WriteFile(path, []byte(rawHex+"\n"), 0o600); err != nil {
		t.Fatalf("write proto golden %s: %v", path, err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
