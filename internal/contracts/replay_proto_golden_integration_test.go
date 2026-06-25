package contracts_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestGoldenReplay_ProtoWireFormat_DeterministicOutput(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	inputPath := filepath.Join("testdata", "replay", "fixtures", "input-mini.jsonl")
	ensureMiniInputFixture(t, inputPath, 50, true)

	r := newTestReader(t, inputPath)
	defer func() { _ = r.Close() }()

	for idx := 0; ; idx++ {
		rec, ok, p := r.Next()
		if p != nil {
			t.Fatalf("Next[%d]: %v", idx, p)
		}
		if !ok {
			break
		}

		original, p := codec.DecodePayload(rec.Envelope.Type, rec.Envelope.Version, rec.Envelope.ContentType, rec.Envelope.Payload)
		if p != nil {
			t.Fatalf("DecodePayload(original)[%d]: %v", idx, p)
		}

		jsonWire, p := codec.EncodePayload(rec.Envelope.Type, rec.Envelope.Version, envelope.ContentTypeJSON, original)
		if p != nil {
			t.Fatalf("EncodePayload(json)[%d]: %v", idx, p)
		}
		jsonDecoded, p := codec.DecodePayload(rec.Envelope.Type, rec.Envelope.Version, envelope.ContentTypeJSON, jsonWire)
		if p != nil {
			t.Fatalf("DecodePayload(json)[%d]: %v", idx, p)
		}

		protoWire, p := codec.EncodePayload(rec.Envelope.Type, rec.Envelope.Version, envelope.ContentTypeProto, original)
		if p != nil {
			t.Fatalf("EncodePayload(proto)[%d]: %v", idx, p)
		}
		protoDecoded, p := codec.DecodePayload(rec.Envelope.Type, rec.Envelope.Version, envelope.ContentTypeProto, protoWire)
		if p != nil {
			t.Fatalf("DecodePayload(proto)[%d]: %v", idx, p)
		}

		if !reflect.DeepEqual(jsonDecoded, protoDecoded) {
			t.Fatalf("semantic drift[%d]: json=%#v proto=%#v", idx, jsonDecoded, protoDecoded)
		}
	}
}
