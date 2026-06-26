package aggruntime

import (
	"context"
	"sort"
	"strings"
	"time"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func (p *ProcessorSubsystemActor) handleBookDeltaForCrossVenue(env envelope.Envelope, instrumentKey string) *problem.Problem {
	if !p.cfg.CrossVenue.Enabled || p.cfg.PublishEnvelope == nil {
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.UpdateBook == nil || p.cfg.CrossVenueMerger == nil {
		return nil
	}

	nowMs := env.TsIngest
	if nowMs <= 0 {
		return problem.New(problem.ValidationFailed, "cross-venue merge requires ts_ingest > 0")
	}

	snapshot, prob := p.cfg.Service.UpdateBook.Snapshot(env.Venue, instrumentKey)
	if prob != nil {
		return prob
	}
	venue := strings.ToUpper(strings.TrimSpace(env.Venue))
	if venue == "" {
		return nil
	}

	venueBook := aggdomain.CrossVenueVenueBook{
		Venue:    venue,
		TsIngest: nowMs,
		Seq:      snapshot.Seq,
	}
	if len(snapshot.Bids) > 0 {
		bid := snapshot.Bids[0]
		venueBook.BestBid = &bid
	}
	if len(snapshot.Asks) > 0 {
		ask := snapshot.Asks[0]
		venueBook.BestAsk = &ask
	}

	p.upsertCrossVenueBook(instrumentKey, venueBook)
	books := p.collectCrossVenueBooks(instrumentKey)

	mergeStartedAt := time.Now()
	merged, mergeProb := p.cfg.CrossVenueMerger.Merge(
		naming.StripMarketType(instrumentKey),
		nowMs,
		books,
		p.cfg.CrossVenue.StaleThreshold.Milliseconds(),
	)
	metrics.ObserveMRXVenueMergeDuration(instrumentKey, time.Since(mergeStartedAt))
	if mergeProb != nil {
		return mergeProb
	}

	metrics.SetMRXVenueSpreadBPS(instrumentKey, merged.GlobalSpreadBPS)
	metrics.SetMRXVenueDivergenceBPS(instrumentKey, merged.VenueDivergenceBPS)
	metrics.SetMRXVenueVenuesActive(instrumentKey, len(merged.BestBids))

	seq := p.nextCrossVenueSeq(instrumentKey)
	outEnv, prob := buildCrossVenueBookEnvelope(env, instrumentKey, seq, merged)
	if prob != nil {
		return prob
	}
	if prob := p.cfg.PublishEnvelope.Publish(context.Background(), outEnv); prob != nil {
		return prob
	}
	return nil
}

func (p *ProcessorSubsystemActor) upsertCrossVenueBook(instrumentKey string, book aggdomain.CrossVenueVenueBook) {
	if p.crossVenueBooks == nil {
		p.crossVenueBooks = make(map[string]map[string]aggdomain.CrossVenueVenueBook)
	}
	if p.crossVenueSeq == nil {
		p.crossVenueSeq = make(map[string]int64)
	}
	venues, ok := p.crossVenueBooks[instrumentKey]
	if !ok {
		p.evictCrossVenueInstrumentIfNeeded()
		venues = make(map[string]aggdomain.CrossVenueVenueBook, p.cfg.CrossVenue.MaxVenues)
		p.crossVenueBooks[instrumentKey] = venues
		p.crossVenueInstrumentQ = append(p.crossVenueInstrumentQ, instrumentKey)
	}

	if _, exists := venues[book.Venue]; !exists && len(venues) >= p.cfg.CrossVenue.MaxVenues {
		evictKey, found := deterministicCrossVenueEvictionCandidate(venues)
		if found && strings.Compare(book.Venue, evictKey) < 0 {
			delete(venues, evictKey)
		} else {
			return
		}
	}
	venues[book.Venue] = book
}

func (p *ProcessorSubsystemActor) evictCrossVenueInstrumentIfNeeded() {
	if p.cfg.CrossVenue.MaxInstruments <= 0 {
		return
	}
	if len(p.crossVenueBooks) < p.cfg.CrossVenue.MaxInstruments {
		return
	}
	if len(p.crossVenueInstrumentQ) == 0 {
		return
	}
	evicted := p.crossVenueInstrumentQ[0]
	p.crossVenueInstrumentQ = p.crossVenueInstrumentQ[1:]
	delete(p.crossVenueBooks, evicted)
	delete(p.crossVenueSeq, evicted)
	metrics.SetMRXVenueVenuesActive(evicted, 0)
}

func deterministicCrossVenueEvictionCandidate(venues map[string]aggdomain.CrossVenueVenueBook) (string, bool) {
	var (
		evict string
		found bool
	)
	for venue := range venues {
		if !found || strings.Compare(venue, evict) > 0 {
			evict = venue
			found = true
		}
	}
	return evict, found
}

func (p *ProcessorSubsystemActor) collectCrossVenueBooks(instrumentKey string) []aggdomain.CrossVenueVenueBook {
	venues := p.crossVenueBooks[instrumentKey]
	if len(venues) == 0 {
		return nil
	}
	venueKeys := make([]string, 0, len(venues))
	for venue := range venues {
		venueKeys = append(venueKeys, venue)
	}
	sort.Strings(venueKeys)
	books := make([]aggdomain.CrossVenueVenueBook, 0, len(venueKeys))
	for _, venue := range venueKeys {
		books = append(books, venues[venue])
	}
	return books
}

func (p *ProcessorSubsystemActor) nextCrossVenueSeq(instrumentKey string) int64 {
	next := p.crossVenueSeq[instrumentKey] + 1
	if next <= 0 {
		next = 1
	}
	p.crossVenueSeq[instrumentKey] = next
	return next
}
