package md_common

// S50: Multi-Venue Fusion & Evidence Overlay
// All fusion logic is backend-driven. These types are display-only read models.

// Fusion_Mode mirrors the backend FusionMode enum.
Fusion_Mode :: enum u8 {
	None,       // not a fused stream
	Single,     // single venue passthrough with meta
	Merge,      // cross-venue merge (union)
	Weighted,   // volume-weighted merge
}

// Fusion_Source describes one venue's contribution.
Fusion_Source :: struct {
	venue:        string,
	weight_pct:   f64,
	last_seen_ms: i64,
	is_stale:     bool,
}

// Fusion_Staleness summarizes freshness across sources.
Fusion_Staleness :: struct {
	fresh_count: int,
	stale_count: int,
	oldest_ms:   i64,
	newest_ms:   i64,
}

// Fusion_Meta carries evidence metadata for any fused payload.
// Parsed from JSON payload — display-only, no client-side fusion logic.
Fusion_Meta :: struct {
	reason:       string,
	confidence:   f64,       // 0.0-1.0
	source_mix:   [FUSION_MAX_SOURCES]Fusion_Source,
	source_count: int,
	staleness:    Fusion_Staleness,
	feature_tags: [FUSION_MAX_TAGS]string,
	tag_count:    int,
}

FUSION_MAX_SOURCES :: 8
FUSION_MAX_TAGS    :: 10

// Fusion_Confidence_Level is a display-friendly confidence classification.
Fusion_Confidence_Level :: enum u8 {
	Unknown,
	High,     // >= 0.9
	Medium,   // >= 0.5
	Low,      // < 0.5
}

// fusion_confidence_level derives display classification from raw confidence.
fusion_confidence_level :: proc(confidence: f64) -> Fusion_Confidence_Level {
	if confidence >= 0.9 do return .High
	if confidence >= 0.5 do return .Medium
	if confidence > 0    do return .Low
	return .Unknown
}

// fusion_has_tag checks if a tag is present in the meta.
fusion_has_tag :: proc(meta: ^Fusion_Meta, tag: string) -> bool {
	for i in 0 ..< meta.tag_count {
		if meta.feature_tags[i] == tag do return true
	}
	return false
}

// fusion_is_degraded returns true if confidence < 0.5.
fusion_is_degraded :: proc(meta: ^Fusion_Meta) -> bool {
	return meta.confidence < 0.5 && meta.confidence > 0
}

// fusion_fresh_ratio returns the ratio of fresh sources.
fusion_fresh_ratio :: proc(meta: ^Fusion_Meta) -> f64 {
	total := meta.staleness.fresh_count + meta.staleness.stale_count
	if total == 0 do return 0
	return f64(meta.staleness.fresh_count) / f64(total)
}

// Fusion_Badge is a display-ready summary for rendering in widget headers.
Fusion_Badge :: struct {
	mode:             Fusion_Mode,
	confidence_level: Fusion_Confidence_Level,
	source_count:     int,
	fresh_count:      int,
	is_degraded:      bool,
}

// resolve_fusion_badge creates a display badge from fusion meta.
resolve_fusion_badge :: proc(meta: ^Fusion_Meta, mode: Fusion_Mode) -> Fusion_Badge {
	return Fusion_Badge{
		mode             = mode,
		confidence_level = fusion_confidence_level(meta.confidence),
		source_count     = meta.source_count,
		fresh_count      = meta.staleness.fresh_count,
		is_degraded      = fusion_is_degraded(meta),
	}
}
