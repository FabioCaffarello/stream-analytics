package contracts

import (
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
)

func TestRegisterFusionPayloadV1(t *testing.T) {
	reg := codec.NewRegistry()
	if p := RegisterFusionPayloadV1(reg); p != nil {
		t.Fatalf("register fusion payloads failed: %v", p)
	}
	if reg.Size() < 3 {
		t.Fatalf("expected at least 3 fusion codecs registered, got %d", reg.Size())
	}
}

func TestRegisterFusionPayloadV1_NilRegistry(t *testing.T) {
	if p := RegisterFusionPayloadV1(nil); p == nil {
		t.Fatal("expected error for nil registry")
	}
}
