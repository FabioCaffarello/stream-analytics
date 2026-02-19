package app

import (
	"context"
	"encoding/json"
	"testing"

	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

func TestHeatmapReplayByteStable50Runs(t *testing.T) {
	var expected string
	for i := 0; i < 50; i++ {
		uc := NewBuildHeatmap()
		var last BuildHeatmapResponse
		for _, req := range testHeatmapSequence() {
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("Execute failed: %v", res.Problem())
			}
			last = res.Value()
		}
		raw, err := json.Marshal(last.Artifact)
		if err != nil {
			t.Fatalf("Marshal artifact: %v", err)
		}
		h := sharedhash.HashBytes(raw)
		if i == 0 {
			expected = h
			continue
		}
		if h != expected {
			t.Fatalf("byte stability mismatch at run=%d got=%s want=%s", i, h, expected)
		}
	}
}
