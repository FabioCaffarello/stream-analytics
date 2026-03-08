package httpserver

import (
	"net/http"
	"time"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
)

// StalenessThresholdMs is the maximum age (in milliseconds) before a portfolio
// projection is considered stale. Matches the client-side STALE_THRESHOLD_MS.
const StalenessThresholdMs = 300_000 // 5 minutes

// handleGetTradingReadiness serves GET /api/v1/trading/readiness.
//
// Composes a unified readiness surface from:
//   - Control plane snapshot (execution bounded context)
//   - Portfolio summary latest (portfolio bounded context)
//   - Account snapshots (portfolio bounded context)
//
// Degrades gracefully: missing dependencies result in partial responses.
// Portfolio does NOT own authorization logic — this handler merely reflects
// the control plane state alongside portfolio staleness.
func (s *Server) handleGetTradingReadiness(w http.ResponseWriter, r *http.Request) {
	nowMs := time.Now().UnixMilli()

	result := executiondomain.TradingReadinessV1{
		EvaluatedAtMs:        nowMs,
		StalenessThresholdMs: StalenessThresholdMs,
		SafetyFlags:          make([]string, 0, 8),
		Accounts:             make([]executiondomain.AccountReadiness, 0),
	}

	// --- Control plane section ---
	baseStatus := executiondomain.TradingStatusEnabled
	var restrictedVenues map[string]struct{}

	if s.controlPlane != nil {
		snap := s.controlPlane.Snapshot()
		baseStatus = executiondomain.BaseTradingStatus(snap.State)

		strategies := make([]string, 0, len(snap.DisabledStrategies))
		for k := range snap.DisabledStrategies {
			strategies = append(strategies, k)
		}
		adapters := make([]string, 0, len(snap.DisabledAdapters))
		for k := range snap.DisabledAdapters {
			adapters = append(adapters, k)
		}

		cp := executiondomain.ControlPlaneReadiness{
			State:              string(snap.State),
			SimulationProfile:  snap.SimulationProfile,
			DisabledStrategies: strategies,
			DisabledAdapters:   adapters,
			UpdatedAtMs:        snap.UpdatedAtMs,
		}

		if snap.AllowlistOverrides != nil {
			if len(snap.AllowlistOverrides.RestrictVenues) > 0 {
				cp.AllowlistRestricted = true
				cp.RestrictedVenues = make([]string, 0, len(snap.AllowlistOverrides.RestrictVenues))
				for k := range snap.AllowlistOverrides.RestrictVenues {
					cp.RestrictedVenues = append(cp.RestrictedVenues, k)
				}
				restrictedVenues = snap.AllowlistOverrides.RestrictVenues
			}
			if len(snap.AllowlistOverrides.RestrictSymbols) > 0 {
				cp.AllowlistRestricted = true
				cp.RestrictedSymbols = make([]string, 0, len(snap.AllowlistOverrides.RestrictSymbols))
				for k := range snap.AllowlistOverrides.RestrictSymbols {
					cp.RestrictedSymbols = append(cp.RestrictedSymbols, k)
				}
			}
		}

		result.ControlPlane = cp

		// Safety flags.
		if snap.SimulationProfile != "" {
			result.SafetyFlags = append(result.SafetyFlags, "simulation")
		}
		switch snap.State {
		case executiondomain.ControlStatePaused:
			result.SafetyFlags = append(result.SafetyFlags, "paused")
		case executiondomain.ControlStateDrained:
			result.SafetyFlags = append(result.SafetyFlags, "drained")
		case executiondomain.ControlStateHalted:
			result.SafetyFlags = append(result.SafetyFlags, "halted")
		}
		if len(snap.DisabledStrategies) > 0 {
			result.SafetyFlags = append(result.SafetyFlags, "strategies_disabled")
		}
		if len(snap.DisabledAdapters) > 0 {
			result.SafetyFlags = append(result.SafetyFlags, "adapters_disabled")
		}
		if cp.AllowlistRestricted {
			result.SafetyFlags = append(result.SafetyFlags, "venue_restricted")
		}

		// If adapters are disabled, degrade to degraded (unless already worse).
		if baseStatus == executiondomain.TradingStatusEnabled && len(snap.DisabledAdapters) > 0 {
			baseStatus = executiondomain.TradingStatusDegraded
		}
	} else {
		result.ControlPlane = executiondomain.ControlPlaneReadiness{
			State:              "unknown",
			DisabledStrategies: make([]string, 0),
			DisabledAdapters:   make([]string, 0),
		}
		result.SafetyFlags = append(result.SafetyFlags, "control_plane_unavailable")
	}

	// --- Portfolio section (accounts + staleness) ---
	if s.portfolioReaders != nil && s.portfolioReaders.Snapshots != nil {
		// Use summary for account list, then snapshots for venue detail.
		if s.portfolioReaders.Summaries != nil {
			summary, p := s.portfolioReaders.Summaries.GetLatestPortfolioSummary(r.Context())
			if p == nil && len(summary.Accounts) > 0 {
				for _, acct := range summary.Accounts {
					ar := executiondomain.AccountReadiness{
						AccountID:     acct.AccountID,
						EquityUSD:     acct.EquityUSD,
						PositionCount: acct.PositionCount,
						Venues:        make([]executiondomain.VenueReadiness, 0),
					}

					// Try to get venue detail from account snapshot.
					snap, snapErr := s.portfolioReaders.Snapshots.GetLatestAccountSnapshot(r.Context(), acct.AccountID)
					if snapErr == nil && snap.SnapshotID != "" {
						acctStale := (nowMs - snap.ProjectedAtMs) > StalenessThresholdMs
						ar.Stale = acctStale

						for _, vp := range snap.Venues {
							if vp.Venue == "" {
								continue
							}
							venueStatus := baseStatus
							venueRestricted := false

							// Check if venue is restricted by allowlist.
							if restrictedVenues != nil {
								if _, ok := restrictedVenues[vp.Venue]; !ok {
									venueRestricted = true
									if venueStatus == executiondomain.TradingStatusEnabled {
										venueStatus = executiondomain.TradingStatusDegraded
									}
								}
							}

							vr := executiondomain.VenueReadiness{
								Venue:           vp.Venue,
								TradingStatus:   venueStatus,
								PositionCount:   int32(len(vp.Positions)),
								EquityUSD:       vp.EquityUSD,
								LastProjectedMs: snap.ProjectedAtMs,
								Stale:           acctStale,
								Restricted:      venueRestricted,
							}
							ar.Venues = append(ar.Venues, vr)
						}
					}

					result.Accounts = append(result.Accounts, ar)
				}
			}
		}
	}

	if len(result.SafetyFlags) == 0 {
		result.SafetyFlags = append(result.SafetyFlags, "clear")
	}

	s.logger.Debug("trading readiness served",
		"accounts", len(result.Accounts),
		"flags", len(result.SafetyFlags),
	)
	writeResponse(w, r, http.StatusOK, "trading.readiness", result)
}
