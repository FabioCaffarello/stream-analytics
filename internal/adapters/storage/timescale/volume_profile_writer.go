package timescale

import (
	"context"
	"strconv"
	"strings"
	"sync"

	insightsports "github.com/market-raccoon/internal/core/insights/ports"
	"github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

// VolumeProfileWriter is a hot-path Timescale writer skeleton for VPVR buckets.
// TODO(sql/timescale/insights_volume_profile_hot.sql): create table and ON CONFLICT upsert.
// ACK boundary is external and must use CommitAndAck in adapter flow (VPVR-STO-4).
type VolumeProfileWriter struct {
	mu      sync.RWMutex
	rows    map[vpvrStorageKey]insightsports.VolumeProfileBucketUpsert
	seenOps map[vpvrStorageKey]map[string]struct{}
	commits int
}

type vpvrStorageKey struct {
	venue         string
	instrument    string
	timeframe     string
	windowStartTs int64
	bucketLow     string
	bucketHigh    string
}

var _ insightsports.VolumeProfileHotWriter = (*VolumeProfileWriter)(nil)

func NewVolumeProfileWriter() *VolumeProfileWriter {
	return &VolumeProfileWriter{
		rows:    make(map[vpvrStorageKey]insightsports.VolumeProfileBucketUpsert),
		seenOps: make(map[vpvrStorageKey]map[string]struct{}),
	}
}

func (w *VolumeProfileWriter) UpsertVolumeProfileBucket(_ context.Context, upsert insightsports.VolumeProfileBucketUpsert) *problem.Problem {
	if w == nil {
		return problem.New(problem.ValidationFailed, "timescale volume profile writer is nil")
	}
	if p := upsert.Validate(); p != nil {
		return p
	}

	norm := normalizeVPVRUpsert(upsert)
	key := storageKeyFromUpsert(norm)
	fp := operationFingerprint(norm)

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.seenOps[key]; !ok {
		w.seenOps[key] = make(map[string]struct{})
	}
	if _, dup := w.seenOps[key][fp]; dup {
		return nil
	}

	if existing, ok := w.rows[key]; ok {
		merged := mergeUpsert(existing, norm)
		w.rows[key] = merged
	} else {
		w.rows[key] = norm
	}
	w.seenOps[key][fp] = struct{}{}
	w.commits++
	return nil
}

func (w *VolumeProfileWriter) CommitCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.commits
}

func (w *VolumeProfileWriter) RowCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.rows)
}

func (w *VolumeProfileWriter) ReadByKey(venue, instrument, timeframe string, windowStartTs int64, bucketLow, bucketHigh float64) (insightsports.VolumeProfileBucketUpsert, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	key := vpvrStorageKey{
		venue:         naming.CanonicalVenue(venue),
		instrument:    naming.CanonicalInstrument(instrument),
		timeframe:     strings.ToLower(strings.TrimSpace(timeframe)),
		windowStartTs: windowStartTs,
		bucketLow:     strconv.FormatFloat(bucketLow, 'f', -1, 64),
		bucketHigh:    strconv.FormatFloat(bucketHigh, 'f', -1, 64),
	}
	row, ok := w.rows[key]
	return row, ok
}

func normalizeVPVRUpsert(in insightsports.VolumeProfileBucketUpsert) insightsports.VolumeProfileBucketUpsert {
	return insightsports.VolumeProfileBucketUpsert{
		Venue:         naming.CanonicalVenue(in.Venue),
		Instrument:    naming.CanonicalInstrument(in.Instrument),
		Timeframe:     strings.ToLower(strings.TrimSpace(in.Timeframe)),
		WindowStartTs: in.WindowStartTs,
		BucketLow:     in.BucketLow,
		BucketHigh:    in.BucketHigh,
		BuyVolume:     in.BuyVolume,
		SellVolume:    in.SellVolume,
		TotalVolume:   in.TotalVolume,
		SeqMin:        in.SeqMin,
		SeqMax:        in.SeqMax,
	}
}

func storageKeyFromUpsert(in insightsports.VolumeProfileBucketUpsert) vpvrStorageKey {
	return vpvrStorageKey{
		venue:         in.Venue,
		instrument:    in.Instrument,
		timeframe:     in.Timeframe,
		windowStartTs: in.WindowStartTs,
		bucketLow:     strconv.FormatFloat(in.BucketLow, 'f', -1, 64),
		bucketHigh:    strconv.FormatFloat(in.BucketHigh, 'f', -1, 64),
	}
}

func operationFingerprint(in insightsports.VolumeProfileBucketUpsert) string {
	return hash.HashFields(
		strconv.FormatFloat(in.BuyVolume, 'f', -1, 64),
		strconv.FormatFloat(in.SellVolume, 'f', -1, 64),
		strconv.FormatFloat(in.TotalVolume, 'f', -1, 64),
		strconv.FormatInt(in.SeqMin, 10),
		strconv.FormatInt(in.SeqMax, 10),
	)
}

func mergeUpsert(existing, incoming insightsports.VolumeProfileBucketUpsert) insightsports.VolumeProfileBucketUpsert {
	out := existing
	out.BuyVolume += incoming.BuyVolume
	out.SellVolume += incoming.SellVolume
	out.TotalVolume += incoming.TotalVolume
	if incoming.SeqMin < out.SeqMin {
		out.SeqMin = incoming.SeqMin
	}
	if incoming.SeqMax > out.SeqMax {
		out.SeqMax = incoming.SeqMax
	}
	return out
}
