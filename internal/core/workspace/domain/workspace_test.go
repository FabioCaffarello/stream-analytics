package domain

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestNewFromPayload_Valid(t *testing.T) {
	w, p := NewFromPayload(10, "V6|a|b", map[string]string{"theme": "dark"}, 0)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if w.SchemaVersion() != 10 {
		t.Errorf("schema_version: expected 10, got %d", w.SchemaVersion())
	}
	if w.LayoutV6() != "V6|a|b" {
		t.Errorf("layout_v6: expected V6|a|b, got %s", w.LayoutV6())
	}
	if w.Fingerprint() == "" {
		t.Error("fingerprint should be non-empty")
	}
}

func TestNewFromPayload_SchemaVersionZero(t *testing.T) {
	_, p := NewFromPayload(0, "V6|a", nil, 0)
	if p == nil {
		t.Fatal("expected problem for schema_version=0")
	}
	if p.Code != problem.ValidationFailed {
		t.Errorf("expected ValidationFailed, got %v", p.Code)
	}
}

func TestNewFromPayload_SchemaVersionNegative(t *testing.T) {
	_, p := NewFromPayload(-1, "V6|a", nil, 0)
	if p == nil {
		t.Fatal("expected problem for negative schema_version")
	}
}

func TestNewFromPayload_FutureSchemaVersion(t *testing.T) {
	_, p := NewFromPayload(MaxSchemaVersion+1, "V6|a", nil, 0)
	if p == nil {
		t.Fatal("expected problem for future schema_version")
	}
	if p.Code != problem.Conflict {
		t.Errorf("expected Conflict, got %v", p.Code)
	}
}

func TestNewFromPayload_EmptyLayout(t *testing.T) {
	_, p := NewFromPayload(10, "", nil, 0)
	if p == nil {
		t.Fatal("expected problem for empty layout")
	}
}

func TestNewFromPayload_InvalidLayoutPrefix(t *testing.T) {
	_, p := NewFromPayload(10, "INVALID|a", nil, 0)
	if p == nil {
		t.Fatal("expected problem for invalid layout prefix")
	}
}

func TestNewFromPayload_BareV6Prefix(t *testing.T) {
	w, p := NewFromPayload(10, "V6", nil, 0)
	if p != nil {
		t.Fatalf("unexpected problem for bare V6: %v", p)
	}
	if w.LayoutV6() != "V6" {
		t.Errorf("expected V6, got %s", w.LayoutV6())
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	settings := map[string]string{"theme": "dark", "zoom": "1.5"}
	w1, _ := NewFromPayload(10, "V6|a|b", settings, 0)
	w2, _ := NewFromPayload(10, "V6|a|b", settings, 0)

	if w1.Fingerprint() != w2.Fingerprint() {
		t.Errorf("identical inputs should produce same fingerprint: %s vs %s",
			w1.Fingerprint(), w2.Fingerprint())
	}
}

func TestFingerprint_DifferentLayout(t *testing.T) {
	w1, _ := NewFromPayload(10, "V6|a", nil, 0)
	w2, _ := NewFromPayload(10, "V6|b", nil, 0)

	if w1.Fingerprint() == w2.Fingerprint() {
		t.Error("different layouts should produce different fingerprints")
	}
}

func TestFingerprint_DifferentSettings(t *testing.T) {
	w1, _ := NewFromPayload(10, "V6|a", map[string]string{"k": "v1"}, 0)
	w2, _ := NewFromPayload(10, "V6|a", map[string]string{"k": "v2"}, 0)

	if w1.Fingerprint() == w2.Fingerprint() {
		t.Error("different settings should produce different fingerprints")
	}
}

func TestHasSameFingerprint(t *testing.T) {
	w1, _ := NewFromPayload(10, "V6|a", map[string]string{"k": "v"}, 0)
	w2, _ := NewFromPayload(10, "V6|a", map[string]string{"k": "v"}, 0)
	w3, _ := NewFromPayload(10, "V6|b", nil, 0)

	if !w1.HasSameFingerprint(w2) {
		t.Error("same content should have same fingerprint")
	}
	if w1.HasSameFingerprint(w3) {
		t.Error("different content should have different fingerprint")
	}
	if w1.HasSameFingerprint(nil) {
		t.Error("nil comparison should return false")
	}
}

func TestStampSaveTime_SetsWhenZero(t *testing.T) {
	w, _ := NewFromPayload(10, "V6|a", nil, 0)
	ts := w.StampSaveTime(1709971200000)
	if ts != 1709971200000 {
		t.Errorf("expected stamped time, got %d", ts)
	}
	if w.SavedAtMs() != 1709971200000 {
		t.Errorf("SavedAtMs not updated: %d", w.SavedAtMs())
	}
}

func TestStampSaveTime_PreservesExisting(t *testing.T) {
	w, _ := NewFromPayload(10, "V6|a", nil, 1000)
	ts := w.StampSaveTime(2000)
	if ts != 1000 {
		t.Errorf("should preserve existing timestamp, got %d", ts)
	}
}

func TestSettings_ReturnsCopy(t *testing.T) {
	w, _ := NewFromPayload(10, "V6|a", map[string]string{"k": "v"}, 0)
	cp := w.Settings()
	cp["mutated"] = "yes"
	if _, exists := w.Settings()["mutated"]; exists {
		t.Error("Settings() should return a copy, not a reference")
	}
}

func TestReconstitute(t *testing.T) {
	w := Reconstitute(10, "V6|a", "fp123", map[string]string{"k": "v"}, 1000)
	if w.SchemaVersion() != 10 {
		t.Errorf("expected 10, got %d", w.SchemaVersion())
	}
	if w.Fingerprint() != "fp123" {
		t.Errorf("expected fp123, got %s", w.Fingerprint())
	}
}
