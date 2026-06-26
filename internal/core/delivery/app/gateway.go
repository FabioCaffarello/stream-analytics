package app

import (
	"strings"
	"sync"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// GatewayFilter defines terminal subscription constraints.
type GatewayFilter struct {
	Venue       string
	Symbol      string
	Channel     string
	Depth       uint32
	Aggregation string
}

// GatewayStream is one active stream projection for a client filter.
type GatewayStream struct {
	StreamID string
	Filter   GatewayFilter
	LastSeq  int64
}

// GatewayEvent is a normalized publish input for the market-data gateway.
type GatewayEvent struct {
	StreamID    string
	Seq         int64
	TsServer    int64
	Venue       string
	Symbol      string
	Channel     string
	Depth       uint32
	Aggregation string
}

// MarketDataGateway provides deterministic publish/subscribe fanout indexing.
// It enforces monotonic sequence ordering per stream and deduplicates repeats.
type MarketDataGateway struct {
	mu       sync.RWMutex
	streams  map[string]GatewayFilter
	lastSeq  map[string]int64
	snapshot map[string]GatewayEvent
}

func NewMarketDataGateway() *MarketDataGateway {
	return &MarketDataGateway{
		streams:  make(map[string]GatewayFilter),
		lastSeq:  make(map[string]int64),
		snapshot: make(map[string]GatewayEvent),
	}
}

// Subscribe registers one filter and returns its deterministic stream id.
func (g *MarketDataGateway) Subscribe(filter GatewayFilter) (GatewayStream, *problem.Problem) {
	filter = normalizeFilter(filter)
	if p := validateFilter(filter); p != nil {
		return GatewayStream{}, p
	}
	streamID := buildStreamID(filter)
	g.mu.Lock()
	g.streams[streamID] = filter
	last := g.lastSeq[streamID]
	g.mu.Unlock()
	return GatewayStream{StreamID: streamID, Filter: filter, LastSeq: last}, nil
}

func (g *MarketDataGateway) Unsubscribe(streamID string) {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return
	}
	g.mu.Lock()
	delete(g.streams, streamID)
	g.mu.Unlock()
}

// Publish applies monotonic sequence checks and returns matching stream list.
func (g *MarketDataGateway) Publish(event GatewayEvent) ([]GatewayStream, *problem.Problem) {
	event = normalizeEvent(event)
	if event.StreamID == "" {
		f := GatewayFilter{
			Venue:       event.Venue,
			Symbol:      event.Symbol,
			Channel:     event.Channel,
			Depth:       event.Depth,
			Aggregation: event.Aggregation,
		}
		if p := validateFilter(f); p != nil {
			return nil, p
		}
		event.StreamID = buildStreamID(f)
	}
	if event.Seq <= 0 {
		return nil, problem.New(problem.ValidationFailed, "seq must be > 0")
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if last := g.lastSeq[event.StreamID]; event.Seq <= last {
		return nil, nil
	}
	g.lastSeq[event.StreamID] = event.Seq
	g.snapshot[event.StreamID] = event

	out := make([]GatewayStream, 0, len(g.streams))
	for streamID, filter := range g.streams {
		if !filterMatchesEvent(filter, event) {
			continue
		}
		out = append(out, GatewayStream{
			StreamID: streamID,
			Filter:   filter,
			LastSeq:  event.Seq,
		})
	}
	return out, nil
}

func (g *MarketDataGateway) Snapshot(streamID string) (GatewayEvent, bool) {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return GatewayEvent{}, false
	}
	g.mu.RLock()
	evt, ok := g.snapshot[streamID]
	g.mu.RUnlock()
	return evt, ok
}

func filterMatchesEvent(filter GatewayFilter, event GatewayEvent) bool {
	if filter.Venue != "" && filter.Venue != event.Venue {
		return false
	}
	if filter.Symbol != "" && filter.Symbol != event.Symbol {
		return false
	}
	if filter.Channel != "" && filter.Channel != event.Channel {
		return false
	}
	if filter.Aggregation != "" && filter.Aggregation != event.Aggregation {
		return false
	}
	if filter.Depth > 0 && event.Depth > 0 && filter.Depth != event.Depth {
		return false
	}
	return true
}

func normalizeFilter(filter GatewayFilter) GatewayFilter {
	return GatewayFilter{
		Venue:       strings.ToLower(strings.TrimSpace(filter.Venue)),
		Symbol:      strings.ToUpper(strings.TrimSpace(filter.Symbol)),
		Channel:     strings.ToLower(strings.TrimSpace(filter.Channel)),
		Depth:       filter.Depth,
		Aggregation: strings.ToLower(strings.TrimSpace(filter.Aggregation)),
	}
}

func normalizeEvent(event GatewayEvent) GatewayEvent {
	event.Venue = strings.ToLower(strings.TrimSpace(event.Venue))
	event.Symbol = strings.ToUpper(strings.TrimSpace(event.Symbol))
	event.Channel = strings.ToLower(strings.TrimSpace(event.Channel))
	event.Aggregation = strings.ToLower(strings.TrimSpace(event.Aggregation))
	event.StreamID = strings.TrimSpace(event.StreamID)
	return event
}

func validateFilter(filter GatewayFilter) *problem.Problem {
	if filter.Venue == "" {
		return problem.New(problem.ValidationFailed, "venue must not be empty")
	}
	if filter.Symbol == "" {
		return problem.New(problem.ValidationFailed, "symbol must not be empty")
	}
	if filter.Channel == "" {
		return problem.New(problem.ValidationFailed, "channel must not be empty")
	}
	if filter.Depth > 10_000 {
		return problem.New(problem.ValidationFailed, "depth too large")
	}
	return nil
}

func buildStreamID(filter GatewayFilter) string {
	streamID := filter.Channel + "/" + filter.Venue + "/" + filter.Symbol
	if filter.Aggregation != "" {
		streamID += "/" + filter.Aggregation
	}
	if filter.Depth > 0 {
		streamID += "/d" + itoa(filter.Depth)
	}
	return streamID
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	buf := [10]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	return string(buf[i:])
}
