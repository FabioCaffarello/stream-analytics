package md_common

// S51: Workspace Governance — fingerprint, compatibility matrix, and version
// guards for safe workspace schema evolution.
//
// Pure functions, no allocations, no mutable state.

WORKSPACE_MIN_COMPAT_VERSION :: 4     // oldest loadable version
WORKSPACE_MAX_COMPAT_VERSION :: 7     // newest understood version

Workspace_Compat_Result :: enum u8 {
	Compatible,           // exact match or within current range
	Upgrade_Available,    // older version, can migrate with defaults
	Downgrade_Warning,    // newer version, load with defaults for unknown fields
	Incompatible,         // below min compat — reject load
}

// workspace_fingerprint computes FNV-1a hash of layout bytes.
// Used for layout identity and change detection across sessions.
workspace_fingerprint :: proc(layout_bytes: []u8) -> u64 {
	FNV_OFFSET :: u64(14695981039346656037)
	FNV_PRIME  :: u64(1099511628211)

	h := FNV_OFFSET
	for b in layout_bytes {
		h ~= u64(b)
		h *= FNV_PRIME
	}
	return h
}

// workspace_compat_check validates a file's schema version against the
// compatibility matrix. Returns the compatibility result.
workspace_compat_check :: proc(file_version: int) -> Workspace_Compat_Result {
	if file_version < WORKSPACE_MIN_COMPAT_VERSION {
		return .Incompatible
	}
	if file_version > WORKSPACE_MAX_COMPAT_VERSION {
		return .Downgrade_Warning
	}
	// Current version is 7 (S51). Older compatible versions get Upgrade_Available.
	if file_version < WORKSPACE_MAX_COMPAT_VERSION {
		return .Upgrade_Available
	}
	return .Compatible
}

// workspace_profile_version_guard gates workspace load on version + fingerprint.
// Returns (compat_result, fingerprint_match).
workspace_profile_version_guard :: proc(
	file_version: int,
	file_fingerprint: u64,
	expected_fingerprint: u64,
) -> (Workspace_Compat_Result, bool) {
	compat := workspace_compat_check(file_version)
	fp_match := file_fingerprint == expected_fingerprint
	return compat, fp_match
}

// workspace_compat_is_loadable returns true if the workspace can be loaded
// (possibly with migration or defaults).
workspace_compat_is_loadable :: proc(result: Workspace_Compat_Result) -> bool {
	switch result {
	case .Compatible, .Upgrade_Available, .Downgrade_Warning:
		return true
	case .Incompatible:
		return false
	}
	return false
}
