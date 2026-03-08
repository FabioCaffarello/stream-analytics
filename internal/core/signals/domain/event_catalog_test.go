package domain

import "testing"

func TestCompositionEventCatalog_AllEntriesHaveRequiredFields(t *testing.T) {
	for _, entry := range CompositionEventCatalog {
		if entry.Type == "" {
			t.Fatal("composition event catalog entry has empty type")
		}
		if entry.Version <= 0 {
			t.Fatalf("composition event catalog entry %q has invalid version %d", entry.Type, entry.Version)
		}
		if entry.OwnerBC == "" {
			t.Fatalf("composition event catalog entry %q has empty owner BC", entry.Type)
		}
		if entry.ProducerBC == "" {
			t.Fatalf("composition event catalog entry %q has empty producer BC", entry.Type)
		}
	}
}

func TestCompositionEventCatalog_NoDuplicateTypes(t *testing.T) {
	seen := make(map[string]struct{}, len(CompositionEventCatalog))
	for _, entry := range CompositionEventCatalog {
		if _, ok := seen[entry.Type]; ok {
			t.Fatalf("duplicate composition event type: %q", entry.Type)
		}
		seen[entry.Type] = struct{}{}
	}
}

func TestCompositionEventCatalogByType_ReturnsAllEntries(t *testing.T) {
	byType := CompositionEventCatalogByType()
	if len(byType) != len(CompositionEventCatalog) {
		t.Fatalf("expected %d entries, got %d", len(CompositionEventCatalog), len(byType))
	}
}
