package policykit

import (
	"regexp"
	"strings"
)

// Category groups stream semantics for overload behavior.
type Category int

const (
	CategoryUnknown Category = iota
	CategoryDelta
	CategorySnapshot
	CategoryCloseFinal
	CategoryTelemetry
)

var versionTokenPattern = regexp.MustCompile(`^v[0-9]+$`)

// CategoryResolver resolves a subject into an overload category.
type CategoryResolver struct {
	bySubject   map[string]Category
	byEventType map[string]Category
}

func NewCategoryResolver() CategoryResolver {
	return CategoryResolver{
		bySubject: map[string]Category{},
		byEventType: map[string]Category{
			"marketdata.bookdelta":                 CategoryDelta,
			"insights.heatmap_delta":               CategoryDelta,
			"insights.volume_profile_delta":        CategoryDelta,
			"insights.heatmap_snapshot":            CategorySnapshot,
			"insights.volume_profile_snapshot":     CategorySnapshot,
			"insights.crossvenue.trade_snapshot":   CategorySnapshot,
			"aggregation.snapshot":                 CategorySnapshot,
			"marketdata.telemetry":                 CategoryTelemetry,
			"runtime.telemetry":                    CategoryTelemetry,
			"insights.volume_profile_window_close": CategoryCloseFinal,
			"insights.volume_profile_final":        CategoryCloseFinal,
		},
	}
}

func (r CategoryResolver) WithSubject(subject string, category Category) CategoryResolver {
	next := r.clone()
	next.bySubject[strings.ToLower(strings.TrimSpace(subject))] = category
	return next
}

func (r CategoryResolver) WithEventType(eventType string, category Category) CategoryResolver {
	next := r.clone()
	next.byEventType[strings.ToLower(strings.TrimSpace(eventType))] = category
	return next
}

func (r CategoryResolver) ResolveSubject(subject string) Category {
	normalized := strings.ToLower(strings.TrimSpace(subject))
	if normalized == "" {
		return CategoryUnknown
	}
	if cat, ok := r.bySubject[normalized]; ok {
		return cat
	}
	eventType := eventTypeFromSubject(normalized)
	if cat, ok := r.byEventType[eventType]; ok {
		return cat
	}
	return inferCategory(eventType)
}

func (r CategoryResolver) clone() CategoryResolver {
	bySubject := make(map[string]Category, len(r.bySubject))
	for k, v := range r.bySubject {
		bySubject[k] = v
	}
	byEventType := make(map[string]Category, len(r.byEventType))
	for k, v := range r.byEventType {
		byEventType[k] = v
	}
	return CategoryResolver{bySubject: bySubject, byEventType: byEventType}
}

func inferCategory(eventType string) Category {
	// Final/close wins over other hints.
	if strings.Contains(eventType, ".close") ||
		strings.Contains(eventType, ".final") ||
		strings.HasSuffix(eventType, "_close") ||
		strings.HasSuffix(eventType, "_final") ||
		strings.Contains(eventType, "window_close") {
		return CategoryCloseFinal
	}
	if strings.Contains(eventType, "telemetry") {
		return CategoryTelemetry
	}
	if strings.Contains(eventType, ".snapshot") || strings.HasSuffix(eventType, "_snapshot") {
		return CategorySnapshot
	}
	if strings.Contains(eventType, ".delta") || strings.HasSuffix(eventType, "_delta") || strings.HasSuffix(eventType, "bookdelta") {
		return CategoryDelta
	}
	return CategoryUnknown
}

func eventTypeFromSubject(subject string) string {
	head := subject
	if idx := strings.IndexByte(subject, '/'); idx >= 0 {
		head = subject[:idx]
	}
	parts := strings.Split(head, ".")
	for i, part := range parts {
		if versionTokenPattern.MatchString(part) {
			if i == 0 {
				return ""
			}
			return strings.Join(parts[:i], ".")
		}
	}
	return head
}
