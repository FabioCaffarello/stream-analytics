package contracts_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/contracts"
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

func writeProtoGoldenHex(t *testing.T, path string, msg proto.Message) {
	t.Helper()
	rawHex := marshalProtoDeterministicHex(t, msg)
	if err := os.WriteFile(path, []byte(rawHex+"\n"), 0o600); err != nil {
		t.Fatalf("write proto golden %s: %v", path, err)
	}
}
