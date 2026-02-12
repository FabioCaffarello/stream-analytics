package ids_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/ids"
)

func TestNewIDs_unique(t *testing.T) {
	a := ids.NewSessionID()
	b := ids.NewSessionID()
	if a == b {
		t.Error("two consecutive IDs must not be equal")
	}
}

func TestNewIDs_format(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"session", string(ids.NewSessionID())},
		{"correlation", string(ids.NewCorrelationID())},
		{"request", string(ids.NewRequestID())},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.id) != 36 {
				t.Errorf("expected 36 chars, got %d", len(tc.id))
			}
			// Check dashes at correct positions
			for _, pos := range []int{8, 13, 18, 23} {
				if tc.id[pos] != '-' {
					t.Errorf("expected '-' at position %d, got %q", pos, tc.id[pos])
				}
			}
		})
	}
}

func TestParseSessionID(t *testing.T) {
	t.Run("valid uuid", func(t *testing.T) {
		raw := string(ids.NewSessionID())
		id, p := ids.ParseSessionID(raw)
		if p != nil {
			t.Errorf("unexpected problem: %s", p)
		}
		if string(id) != raw {
			t.Error("parsed value mismatch")
		}
	})

	t.Run("empty", func(t *testing.T) {
		_, p := ids.ParseSessionID("")
		if p == nil {
			t.Error("expected problem for empty input")
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		_, p := ids.ParseSessionID("not-a-uuid")
		if p == nil {
			t.Error("expected problem for invalid format")
		}
	})
}

func TestParseCorrelationID(t *testing.T) {
	raw := string(ids.NewCorrelationID())
	id, p := ids.ParseCorrelationID(raw)
	if p != nil {
		t.Errorf("unexpected problem: %s", p)
	}
	if string(id) != raw {
		t.Error("round-trip failed")
	}
}
