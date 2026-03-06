package main

import "testing"

func TestEffectiveStrategistFilters_DefaultsToCanonicalSignalEvent(t *testing.T) {
	filters := effectiveStrategistFilters(nil)
	if len(filters) != 1 || filters[0] != "signal.event.>" {
		t.Fatalf("filters=%v want=[signal.event.>]", filters)
	}
}

func TestEffectiveStrategistFilters_StripsLegacyCompositeAndBroadSignal(t *testing.T) {
	filters := effectiveStrategistFilters([]string{
		"signal.composite.>",
		"signal.>",
		"signal.event.>",
	})
	if len(filters) != 1 || filters[0] != "signal.event.>" {
		t.Fatalf("filters=%v want=[signal.event.>]", filters)
	}

	filters = effectiveStrategistFilters([]string{"signal.>"})
	if len(filters) != 1 || filters[0] != "signal.event.>" {
		t.Fatalf("filters from broad signal=%v want=[signal.event.>]", filters)
	}
}
