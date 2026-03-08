package domain

import "testing"

func TestEventCatalog_AllEntriesHaveRequiredFields(t *testing.T) {
	for _, e := range EventCatalog {
		if e.Type == "" {
			t.Fatal("event catalog entry has empty type")
		}
		if e.Version <= 0 {
			t.Fatalf("event %q has invalid version %d", e.Type, e.Version)
		}
		if e.OwnerBC == "" {
			t.Fatalf("event %q has empty owner_bc", e.Type)
		}
		if e.ProducerBC == "" {
			t.Fatalf("event %q has empty producer_bc", e.Type)
		}
	}
}

func TestEventCatalog_NoDuplicateTypes(t *testing.T) {
	seen := make(map[string]struct{}, len(EventCatalog))
	for _, e := range EventCatalog {
		if _, ok := seen[e.Type]; ok {
			t.Fatalf("duplicate event type %q in catalog", e.Type)
		}
		seen[e.Type] = struct{}{}
	}
}

func TestEventCatalogByType_LookupWorks(t *testing.T) {
	m := EventCatalogByType()
	if len(m) != len(EventCatalog) {
		t.Fatalf("map size %d != catalog size %d", len(m), len(EventCatalog))
	}
	for _, e := range EventCatalog {
		got, ok := m[e.Type]
		if !ok {
			t.Fatalf("type %q not found in map", e.Type)
		}
		if got.Version != e.Version {
			t.Fatalf("type %q version mismatch: %d != %d", e.Type, got.Version, e.Version)
		}
	}
}

func TestEventCatalog_AllInsightsOwned(t *testing.T) {
	for _, e := range EventCatalog {
		if e.OwnerBC != "insights" {
			t.Fatalf("event %q owner_bc=%q, want insights", e.Type, e.OwnerBC)
		}
	}
}

func TestEventCatalog_CountIs10(t *testing.T) {
	if got := len(EventCatalog); got != 10 {
		t.Fatalf("catalog has %d entries, want 10", got)
	}
}
