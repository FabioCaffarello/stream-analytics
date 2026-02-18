package contracts

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
)

type subjectRegistryEntry struct {
	ID     string
	Root   string
	Status string
}

func TestPayloadRegistryCoverage_SubjectRegistryStableDraft(t *testing.T) {
	t.Parallel()

	reg := codec.NewRegistry()
	if p := RegisterMarketDataPayloadV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataPayloadV1: %v", p)
	}
	if p := RegisterInsightsPayloadV1WithOptions(reg, InsightsCodecOptions{}); p != nil {
		t.Fatalf("RegisterInsightsPayloadV1WithOptions: %v", p)
	}
	if p := RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}

	// Aggregation subjects that use non-standard payload schemas (not candle/stats).
	aggregationSkip := map[string]bool{
		"aggregation.snapshot":                true,
		"aggregation.orderbook_inconsistency": true,
	}

	subjects := readSubjectRegistry(t)
	for _, subject := range subjects {
		if subject.Status != "stable" && subject.Status != "draft" {
			continue
		}
		if subject.Root != "marketdata" && subject.Root != "insights" && subject.Root != "aggregation" {
			continue
		}
		// Delta payloads are not typed contracts yet in this phase.
		if strings.Contains(subject.ID, ".heatmap_delta.") || strings.Contains(subject.ID, ".volume_profile_delta.") {
			continue
		}

		eventType, version, ok := parseSubjectEventTypeVersion(subject.ID)
		if !ok {
			t.Fatalf("invalid subject id format %q", subject.ID)
		}
		if aggregationSkip[eventType] {
			continue
		}
		if !hasRegisteredCodec(reg, eventType, version) {
			t.Fatalf("forgot to register codec for subject %s", subject.ID)
		}
	}
}

func hasRegisteredCodec(reg *codec.Registry, eventType string, version int32) bool {
	for _, format := range []codec.Format{codec.FormatJSON, codec.FormatProto} {
		key := codec.SchemaKey{Type: eventType, Version: version, Format: format}
		if _, ok := reg.Encoder(key); ok {
			return true
		}
		if _, ok := reg.Decoder(key); ok {
			return true
		}
	}
	return false
}

func parseSubjectEventTypeVersion(id string) (string, int32, bool) {
	parts := strings.Split(strings.TrimSpace(id), ".")
	if len(parts) < 3 {
		return "", 0, false
	}
	verToken := parts[len(parts)-1]
	if len(verToken) < 2 || verToken[0] != 'v' {
		return "", 0, false
	}
	v, err := parseVersionToken(verToken[1:])
	if err != nil || v <= 0 {
		return "", 0, false
	}
	return strings.Join(parts[:len(parts)-1], "."), v, true
}

func readSubjectRegistry(t *testing.T) []subjectRegistryEntry {
	t.Helper()

	path := filepath.Join(findRepoRoot(t), "docs", "contracts", "subject-registry.yaml")
	// #nosec G304 -- path resolves to repository-owned fixed location.
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open subject registry %s: %v", path, err)
	}
	t.Cleanup(func() {
		if cerr := file.Close(); cerr != nil {
			t.Fatalf("close subject registry %s: %v", path, cerr)
		}
	})

	var out []subjectRegistryEntry
	current := subjectRegistryEntry{}
	inSubjects := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "subjects:" {
			inSubjects = true
			continue
		}
		if !inSubjects {
			continue
		}
		if strings.HasPrefix(line, "- id:") {
			out, current = startSubjectEntry(out, current, strings.TrimSpace(strings.TrimPrefix(line, "- id:")))
			continue
		}
		if current.ID == "" {
			continue
		}
		current = consumeSubjectField(current, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan subject registry %s: %v", path, err)
	}
	if current.ID != "" {
		out = append(out, current)
	}
	if len(out) == 0 {
		t.Fatalf("subject registry %s has no subjects", path)
	}
	return out
}

func parseVersionToken(raw string) (int32, error) {
	v64, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(v64), nil
}

func startSubjectEntry(out []subjectRegistryEntry, current subjectRegistryEntry, id string) ([]subjectRegistryEntry, subjectRegistryEntry) {
	if current.ID != "" {
		out = append(out, current)
	}
	return out, subjectRegistryEntry{ID: id}
}

func consumeSubjectField(current subjectRegistryEntry, line string) subjectRegistryEntry {
	switch {
	case strings.HasPrefix(line, "id:"):
		current.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
	case strings.HasPrefix(line, "root:"):
		current.Root = strings.TrimSpace(strings.TrimPrefix(line, "root:"))
	case strings.HasPrefix(line, "status:"):
		current.Status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
	}
	return current
}
