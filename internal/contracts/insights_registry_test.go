package contracts_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	insightsdomain "github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
)

func TestRegisterInsightsV1_DefaultsToJSONOnly(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterInsightsV1(reg); p != nil {
		t.Fatalf("RegisterInsightsV1: %v", p)
	}

	snapshotKey := codec.SchemaKey{
		Type:    insightsdomain.CrossVenueTradeSnapshotType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Encoder(snapshotKey); !ok {
		t.Fatalf("missing encoder for key %+v", snapshotKey)
	}
	if _, ok := reg.Decoder(snapshotKey); !ok {
		t.Fatalf("missing decoder for key %+v", snapshotKey)
	}

	signalKey := codec.SchemaKey{
		Type:    insightsdomain.CrossVenueSpreadSignalType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Encoder(signalKey); !ok {
		t.Fatalf("missing encoder for key %+v", signalKey)
	}
	if _, ok := reg.Decoder(signalKey); !ok {
		t.Fatalf("missing decoder for key %+v", signalKey)
	}

	windowCloseKey := codec.SchemaKey{
		Type:    "insights.volume_profile_window_close",
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Encoder(windowCloseKey); !ok {
		t.Fatalf("missing encoder for key %+v", windowCloseKey)
	}
	if _, ok := reg.Decoder(windowCloseKey); !ok {
		t.Fatalf("missing decoder for key %+v", windowCloseKey)
	}

	finalKey := codec.SchemaKey{
		Type:    "insights.volume_profile_final",
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Encoder(finalKey); !ok {
		t.Fatalf("missing encoder for key %+v", finalKey)
	}
	if _, ok := reg.Decoder(finalKey); !ok {
		t.Fatalf("missing decoder for key %+v", finalKey)
	}

	vpvrProtoKey := codec.SchemaKey{
		Type:    insightsdomain.VolumeProfileSnapshotType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	if _, ok := reg.Encoder(vpvrProtoKey); ok {
		t.Fatalf("unexpected proto encoder for key %+v", vpvrProtoKey)
	}
	if _, ok := reg.Decoder(vpvrProtoKey); ok {
		t.Fatalf("unexpected proto decoder for key %+v", vpvrProtoKey)
	}
}

func TestRegisterInsightsPayloadV1WithOptions_EnablesVPVRProto(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterInsightsPayloadV1WithOptions(reg, contracts.InsightsCodecOptions{
		EnableVolumeProfileSnapshotProto: true,
	}); p != nil {
		t.Fatalf("RegisterInsightsPayloadV1WithOptions: %v", p)
	}

	vpvrProtoKey := codec.SchemaKey{
		Type:    insightsdomain.VolumeProfileSnapshotType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	enc, ok := reg.Encoder(vpvrProtoKey)
	if !ok {
		t.Fatalf("missing proto encoder for key %+v", vpvrProtoKey)
	}
	dec, ok := reg.Decoder(vpvrProtoKey)
	if !ok {
		t.Fatalf("missing proto decoder for key %+v", vpvrProtoKey)
	}

	in := testVPVRSnapshot()
	raw, p := enc.Encode(in)
	if p != nil {
		t.Fatalf("encode vpvr proto: %v", p)
	}
	if got, want := hex.EncodeToString(raw), readGoldenHex(t); got != want {
		t.Fatalf("vpvr proto golden mismatch\ngot=%s\nwant=%s", got, want)
	}

	outAny, p := dec.Decode(raw)
	if p != nil {
		t.Fatalf("decode vpvr proto: %v", p)
	}
	out, ok := outAny.(insightsdomain.VolumeProfileSnapshotV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", outAny, insightsdomain.VolumeProfileSnapshotV1{})
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("vpvr proto roundtrip mismatch\ngot=%+v\nwant=%+v", out, in)
	}
}

func TestRegisterInsightsPayloadV1WithOptions_VPVRProtoByteStability(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterInsightsPayloadV1WithOptions(reg, contracts.InsightsCodecOptions{
		EnableVolumeProfileSnapshotProto: true,
	}); p != nil {
		t.Fatalf("RegisterInsightsPayloadV1WithOptions: %v", p)
	}

	key := codec.SchemaKey{
		Type:    insightsdomain.VolumeProfileSnapshotType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	enc, ok := reg.Encoder(key)
	if !ok {
		t.Fatalf("missing proto encoder for key %+v", key)
	}
	dec, ok := reg.Decoder(key)
	if !ok {
		t.Fatalf("missing proto decoder for key %+v", key)
	}

	in := testVPVRSnapshot()
	first, p := enc.Encode(in)
	if p != nil {
		t.Fatalf("first encode vpvr proto: %v", p)
	}
	second, p := enc.Encode(in)
	if p != nil {
		t.Fatalf("second encode vpvr proto: %v", p)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("byte stability mismatch: first=%x second=%x", first, second)
	}

	decodedAny, p := dec.Decode(first)
	if p != nil {
		t.Fatalf("decode vpvr proto: %v", p)
	}
	decoded, ok := decodedAny.(insightsdomain.VolumeProfileSnapshotV1)
	if !ok {
		t.Fatalf("decoded type=%T want %T", decodedAny, insightsdomain.VolumeProfileSnapshotV1{})
	}
	third, p := enc.Encode(decoded)
	if p != nil {
		t.Fatalf("re-encode decoded vpvr proto: %v", p)
	}
	if !bytes.Equal(first, third) {
		t.Fatalf("re-encode byte stability mismatch: original=%x reencoded=%x", first, third)
	}
}

func testVPVRSnapshot() insightsdomain.VolumeProfileSnapshotV1 {
	return insightsdomain.VolumeProfileSnapshotV1{
		Venue:         "binance",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1_710_000_000_000,
		WindowEndTs:   1_710_000_060_000,
		Buckets: []insightsdomain.VolumeProfileBucketV1{
			{
				PriceLow:    65000,
				PriceHigh:   65010,
				BuyVolume:   1.25,
				SellVolume:  0.75,
				TotalVolume: 2.00,
				SeqMin:      101,
				SeqMax:      120,
			},
			{
				PriceLow:    65010,
				PriceHigh:   65020,
				BuyVolume:   0.50,
				SellVolume:  1.00,
				TotalVolume: 1.50,
				SeqMin:      121,
				SeqMax:      133,
			},
		},
		POCPrice:      65000,
		ValueAreaLow:  65000,
		ValueAreaHigh: 65020,
	}
}

func readGoldenHex(t *testing.T) string {
	t.Helper()
	const goldenPath = "testdata/golden/insights_volume_profile_snapshot_proto_v1.hex"
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	return strings.TrimSpace(string(raw))
}
