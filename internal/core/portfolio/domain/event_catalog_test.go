package domain

import "testing"

func TestPortfolioEventCatalog_HasStateEvent(t *testing.T) {
	catalog := PortfolioEventCatalogByType()
	entry, ok := catalog[StateEventType]
	if !ok {
		t.Fatal("portfolio.state not found in catalog")
	}
	if entry.Version != StateEventVersion {
		t.Fatalf("version=%d want=%d", entry.Version, StateEventVersion)
	}
	if entry.OwnerBC != "portfolio" {
		t.Fatalf("owner_bc=%q want=portfolio", entry.OwnerBC)
	}
}

func TestPortfolioEventCatalog_NoDuplicateTypes(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range PortfolioEventCatalog {
		if seen[e.Type] {
			t.Fatalf("duplicate event type: %s", e.Type)
		}
		seen[e.Type] = true
	}
}

func TestPortfolioReadModelCatalog_NotEmpty(t *testing.T) {
	if len(PortfolioReadModelCatalog) == 0 {
		t.Fatal("read model catalog must not be empty")
	}
}

// TestPortfolioEventCatalogCompleteness ensures all declared event type
// constants are registered in the canonical event catalog.
func TestPortfolioEventCatalogCompleteness(t *testing.T) {
	byType := PortfolioEventCatalogByType()

	required := []struct {
		eventType string
		version   int
	}{
		{StateEventType, StateEventVersion},
		{AccountSnapshotEventType, AccountSnapshotEventVersion},
		{SummaryEventType, SummaryEventVersion},
	}

	for _, r := range required {
		entry, ok := byType[r.eventType]
		if !ok {
			t.Errorf("event type %q not found in PortfolioEventCatalog", r.eventType)
			continue
		}
		if entry.Version != r.version {
			t.Errorf("event %q: expected version %d, got %d", r.eventType, r.version, entry.Version)
		}
		if entry.OwnerBC != "portfolio" {
			t.Errorf("event %q: expected owner_bc=portfolio, got %s", r.eventType, entry.OwnerBC)
		}
	}
}

// TestEventCatalogMatchesReadModelCatalog cross-validates that all read model
// types listed in PortfolioReadModelCatalog have entries in the event catalog.
func TestEventCatalogMatchesReadModelCatalog(t *testing.T) {
	byType := PortfolioEventCatalogByType()

	for _, rmType := range PortfolioReadModelCatalog {
		if _, ok := byType[rmType]; !ok {
			t.Errorf("read model type %q listed in PortfolioReadModelCatalog but missing from PortfolioEventCatalog", rmType)
		}
	}
}

// TestEventCatalogByTypeLookup verifies O(1) lookup returns correct entries.
func TestEventCatalogByTypeLookup(t *testing.T) {
	byType := PortfolioEventCatalogByType()

	if len(byType) != len(PortfolioEventCatalog) {
		t.Fatalf("expected %d entries in lookup map, got %d", len(PortfolioEventCatalog), len(byType))
	}

	for _, entry := range PortfolioEventCatalog {
		got, ok := byType[entry.Type]
		if !ok {
			t.Errorf("type %q not in lookup map", entry.Type)
			continue
		}
		if got != entry {
			t.Errorf("type %q: lookup mismatch: got %+v, want %+v", entry.Type, got, entry)
		}
	}
}
