package policykit

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCategoryResolverResolveSubject(t *testing.T) {
	resolver := NewCategoryResolver().
		WithSubject("custom.subject.v1/binance/BTCUSDT", CategoryTelemetry)

	tests := []struct {
		subject string
		want    Category
	}{
		{subject: "marketdata.bookdelta.v1.binance.BTCUSDT", want: CategoryDelta},
		{subject: "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m", want: CategorySnapshot},
		{subject: "insights.volume_profile_final.v1/binance/BTCUSDT/1m", want: CategoryCloseFinal},
		{subject: "runtime.telemetry.v1.global.platform", want: CategoryTelemetry},
		{subject: "custom.subject.v1/binance/BTCUSDT", want: CategoryTelemetry},
		{subject: "unknown.event.v1.binance.BTCUSDT", want: CategoryUnknown},
	}

	for _, tc := range tests {
		if got := resolver.ResolveSubject(tc.subject); got != tc.want {
			t.Fatalf("subject=%q got=%d want=%d", tc.subject, got, tc.want)
		}
	}
}

func TestDropAllowedGuard(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionDropDelta}}}
	if DropAllowed(CategoryCloseFinal, decision) {
		t.Fatal("close/final must never be drop-eligible")
	}
	if !DropAllowed(CategoryDelta, decision) {
		t.Fatal("delta must be drop-eligible when DropDelta is active")
	}
}

func TestCategoryResolverDefaultEventTypesAreRegisteredSubjects(t *testing.T) {
	t.Parallel()

	resolver := NewCategoryResolver()
	registryIDs := readSubjectRegistryIDs(t)
	registryEventTypes := registryEventTypesFromIDs(t, registryIDs)

	var eventTypes []string
	for eventType := range resolver.byEventType {
		eventTypes = append(eventTypes, eventType)
	}
	slices.Sort(eventTypes)

	for _, eventType := range eventTypes {
		if strings.Contains(eventType, "telemetry") {
			continue
		}
		if !registryEventTypes[eventType] {
			t.Fatalf("missing registry subject for default event type %q", eventType)
		}
	}
}

func TestCategoryResolverCloseFinalRegistrySubjectsResolveAsCloseFinal(t *testing.T) {
	t.Parallel()

	resolver := NewCategoryResolver()
	registryIDs := readSubjectRegistryIDs(t)

	found := 0
	for _, id := range registryIDs {
		eventType := registryEventTypeFromID(t, id)
		if !isCloseFinalEventType(eventType) {
			continue
		}
		found++
		subject := id + ".binance.BTCUSDT"
		if got := resolver.ResolveSubject(subject); got != CategoryCloseFinal {
			t.Fatalf("subject=%q category=%v want=%v", subject, got, CategoryCloseFinal)
		}
	}
	if found == 0 {
		t.Fatal("no close/final subjects found in registry")
	}
}

func readSubjectRegistryIDs(t *testing.T) []string {
	t.Helper()

	path := filepath.Join(findRepoRoot(t), "docs", "contracts", "subject-registry.yaml")
	// #nosec G304 -- fixed repository-local path.
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open subject registry %s: %v", path, err)
	}
	t.Cleanup(func() {
		if cerr := file.Close(); cerr != nil {
			t.Fatalf("close subject registry %s: %v", path, cerr)
		}
	})

	var ids []string
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
			id := strings.TrimSpace(strings.TrimPrefix(line, "- id:"))
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan subject registry %s: %v", path, err)
	}
	if len(ids) == 0 {
		t.Fatalf("subject registry %s has no subjects", path)
	}
	return ids
}

var subjectIDVersionSuffix = regexp.MustCompile(`\.v[0-9]+$`)

func registryEventTypesFromIDs(t *testing.T, ids []string) map[string]bool {
	t.Helper()
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[registryEventTypeFromID(t, id)] = true
	}
	return out
}

func registryEventTypeFromID(t *testing.T, id string) string {
	t.Helper()
	out := subjectIDVersionSuffix.ReplaceAllString(strings.TrimSpace(id), "")
	if out == "" || out == id {
		t.Fatalf("invalid subject id %q", id)
	}
	return out
}

func isCloseFinalEventType(eventType string) bool {
	return strings.Contains(eventType, "_final") ||
		strings.Contains(eventType, "_close") ||
		strings.Contains(eventType, "window_close")
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.work from %s", dir)
		}
		dir = parent
	}
}
