package signal

import "testing"

func TestDetectionEventCatalog_AllEntriesHaveRequiredFields(t *testing.T) {
	for _, entry := range DetectionEventCatalog {
		if entry.Type == "" {
			t.Fatal("detection event catalog entry has empty type")
		}
		if entry.Version <= 0 {
			t.Fatalf("detection event catalog entry %q has invalid version %d", entry.Type, entry.Version)
		}
		if entry.OwnerBC == "" {
			t.Fatalf("detection event catalog entry %q has empty owner BC", entry.Type)
		}
		if entry.ProducerBC == "" {
			t.Fatalf("detection event catalog entry %q has empty producer BC", entry.Type)
		}
		if entry.RuleID == "" {
			t.Fatalf("detection event catalog entry %q has empty rule ID", entry.Type)
		}
	}
}

func TestDetectionEventCatalog_NoDuplicateTypes(t *testing.T) {
	seen := make(map[string]struct{}, len(DetectionEventCatalog))
	for _, entry := range DetectionEventCatalog {
		if _, ok := seen[entry.Type]; ok {
			t.Fatalf("duplicate detection event type: %q", entry.Type)
		}
		seen[entry.Type] = struct{}{}
	}
}

func TestDetectionEventCatalogByType_ReturnsAllEntries(t *testing.T) {
	byType := DetectionEventCatalogByType()
	if len(byType) != len(DetectionEventCatalog) {
		t.Fatalf("expected %d entries, got %d", len(DetectionEventCatalog), len(byType))
	}
	for _, entry := range DetectionEventCatalog {
		found, ok := byType[entry.Type]
		if !ok {
			t.Fatalf("missing entry for type %q", entry.Type)
		}
		if found.RuleID != entry.RuleID {
			t.Fatalf("rule ID mismatch for %q: got %q, want %q", entry.Type, found.RuleID, entry.RuleID)
		}
	}
}
