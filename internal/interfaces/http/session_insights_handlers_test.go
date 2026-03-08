package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
)

// ---------------------------------------------------------------------------
// Fake InsightsSnapshotter
// ---------------------------------------------------------------------------

type fakeInsightsSnapshotter struct {
	svpResult result.Result[insightsdomain.SessionVolumeProfileV1]
	tpoResult result.Result[insightsdomain.TPOProfileV1]

	lastSVPVenue      string
	lastSVPInstrument string
	lastSVPAnchor     string
	lastTPOVenue      string
	lastTPOInstrument string
	lastTPOAnchor     string
}

func (f *fakeInsightsSnapshotter) SnapshotSessionVolumeProfile(_ context.Context, venue, instrument, anchorLabel string) result.Result[insightsdomain.SessionVolumeProfileV1] {
	f.lastSVPVenue = venue
	f.lastSVPInstrument = instrument
	f.lastSVPAnchor = anchorLabel
	return f.svpResult
}

func (f *fakeInsightsSnapshotter) SnapshotTPOProfile(_ context.Context, venue, instrument, anchorLabel string) result.Result[insightsdomain.TPOProfileV1] {
	f.lastTPOVenue = venue
	f.lastTPOInstrument = instrument
	f.lastTPOAnchor = anchorLabel
	return f.tpoResult
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestServerWithInsights(snap InsightsSnapshotter) *Server {
	s := &Server{
		insightsSnapshotter: snap,
		logger:              slog.Default(),
	}
	mux := http.NewServeMux()
	if snap != nil {
		mux.HandleFunc("GET /api/v1/insights/session-vp", s.handleGetSessionVolumeProfile)
		mux.HandleFunc("GET /api/v1/insights/tpo", s.handleGetTPOProfile)
	}
	s.mux = mux
	s.httpServer = &http.Server{Handler: mux}
	return s
}

func doInsightsRequest(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// GET /api/v1/insights/session-vp
// ---------------------------------------------------------------------------

func TestGetSessionVolumeProfile_Success(t *testing.T) {
	svp := insightsdomain.SessionVolumeProfileV1{
		Venue:         "binance",
		Instrument:    "BTCUSDT",
		POCPrice:      42000.0,
		TotalVolume:   10000.0,
		WindowStartTs: 1700000000000,
		WindowEndTs:   0, // still open
	}
	fake := &fakeInsightsSnapshotter{
		svpResult: result.Ok(svp),
	}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/session-vp?venue=binance&instrument=BTCUSDT&anchor=UTC_DAILY")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.lastSVPVenue != "binance" {
		t.Errorf("expected venue=binance, got %s", fake.lastSVPVenue)
	}
	if fake.lastSVPInstrument != "BTCUSDT" {
		t.Errorf("expected instrument=BTCUSDT, got %s", fake.lastSVPInstrument)
	}
	if fake.lastSVPAnchor != "UTC_DAILY" {
		t.Errorf("expected anchor=UTC_DAILY, got %s", fake.lastSVPAnchor)
	}

	var got SessionVPResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.POCPrice != 42000.0 {
		t.Errorf("expected poc_price=42000, got %f", got.POCPrice)
	}
	if !got.IsActive {
		t.Error("expected is_active=true when WindowEndTs==0")
	}
}

func TestGetSessionVolumeProfile_DefaultAnchor(t *testing.T) {
	fake := &fakeInsightsSnapshotter{
		svpResult: result.Ok(insightsdomain.SessionVolumeProfileV1{
			Venue:         "binance",
			Instrument:    "BTCUSDT",
			WindowStartTs: 1700000000000,
			WindowEndTs:   1700086400000,
		}),
	}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/session-vp?venue=binance&instrument=BTCUSDT")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.lastSVPAnchor != "current" {
		t.Errorf("expected default anchor=current, got %s", fake.lastSVPAnchor)
	}

	var got SessionVPResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// anchor == "current" means is_active should be true regardless of WindowEndTs.
	if !got.IsActive {
		t.Error("expected is_active=true when anchor is current")
	}
}

func TestGetSessionVolumeProfile_MissingVenue(t *testing.T) {
	fake := &fakeInsightsSnapshotter{}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/session-vp?instrument=BTCUSDT")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionVolumeProfile_MissingInstrument(t *testing.T) {
	fake := &fakeInsightsSnapshotter{}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/session-vp?venue=binance")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionVolumeProfile_NotFound(t *testing.T) {
	fake := &fakeInsightsSnapshotter{
		svpResult: result.Fail[insightsdomain.SessionVolumeProfileV1](problem.NotFound, "session vp not found"),
	}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/session-vp?venue=binance&instrument=BTCUSDT")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionVolumeProfile_SnapshotterUnavailable(t *testing.T) {
	srv := newTestServerWithInsights(nil)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/session-vp?venue=binance&instrument=BTCUSDT")

	// Routes not registered when snapshotter is nil; should 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/insights/tpo
// ---------------------------------------------------------------------------

func TestGetTPOProfile_Success(t *testing.T) {
	tpo := insightsdomain.TPOProfileV1{
		Venue:         "binance",
		Instrument:    "BTCUSDT",
		POCPrice:      42000.0,
		ValueAreaHigh: 42500.0,
		ValueAreaLow:  41500.0,
		WindowStartTs: 1700000000000,
		WindowEndTs:   0,
	}
	fake := &fakeInsightsSnapshotter{
		tpoResult: result.Ok(tpo),
	}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/tpo?venue=binance&instrument=BTCUSDT&anchor=CME_RTH")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.lastTPOVenue != "binance" {
		t.Errorf("expected venue=binance, got %s", fake.lastTPOVenue)
	}
	if fake.lastTPOInstrument != "BTCUSDT" {
		t.Errorf("expected instrument=BTCUSDT, got %s", fake.lastTPOInstrument)
	}
	if fake.lastTPOAnchor != "CME_RTH" {
		t.Errorf("expected anchor=CME_RTH, got %s", fake.lastTPOAnchor)
	}

	var got TPOProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.POCPrice != 42000.0 {
		t.Errorf("expected poc_price=42000, got %f", got.POCPrice)
	}
	if !got.IsActive {
		t.Error("expected is_active=true when WindowEndTs==0")
	}
}

func TestGetTPOProfile_MissingInstrument(t *testing.T) {
	fake := &fakeInsightsSnapshotter{}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/tpo?venue=binance")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetTPOProfile_MissingVenue(t *testing.T) {
	fake := &fakeInsightsSnapshotter{}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/tpo?instrument=BTCUSDT")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetTPOProfile_NotFound(t *testing.T) {
	fake := &fakeInsightsSnapshotter{
		tpoResult: result.Fail[insightsdomain.TPOProfileV1](problem.NotFound, "tpo not found"),
	}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/tpo?venue=binance&instrument=BTCUSDT")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetTPOProfile_ClosedSession(t *testing.T) {
	tpo := insightsdomain.TPOProfileV1{
		Venue:         "binance",
		Instrument:    "BTCUSDT",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700086400000, // closed
	}
	fake := &fakeInsightsSnapshotter{
		tpoResult: result.Ok(tpo),
	}
	srv := newTestServerWithInsights(fake)
	rec := doInsightsRequest(t, srv, "/api/v1/insights/tpo?venue=binance&instrument=BTCUSDT&anchor=CME_RTH")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got TPOProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// WindowEndTs != 0 and anchor != "current", so is_active should be false.
	if got.IsActive {
		t.Error("expected is_active=false for closed session with non-current anchor")
	}
}
