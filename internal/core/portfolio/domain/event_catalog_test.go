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
