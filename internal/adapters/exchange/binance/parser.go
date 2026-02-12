package binance

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	VenueBinance = "BINANCE"
)

type streamEnvelope struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type aggTrade struct {
	Event        string `json:"e"`
	EventTimeMs  int64  `json:"E"`
	TradeTimeMs  int64  `json:"T"`
	Symbol       string `json:"s"`
	AggTradeID   int64  `json:"a"`
	PriceRaw     string `json:"p"`
	QuantityRaw  string `json:"q"`
	BuyerIsMaker bool   `json:"m"`
}

type depthUpdate struct {
	Event       string     `json:"e"`
	EventTimeMs int64      `json:"E"`
	Symbol      string     `json:"s"`
	BidsRaw     [][]string `json:"b"`
	AsksRaw     [][]string `json:"a"`
}

// ParseMessage parses Binance WS payload and maps supported messages to app.IngestRequest.
// Returns skip=true for unsupported/heartbeat/control messages.
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	payload := data

	// Binance combined stream wraps payload as {stream, data}.
	var wrapped streamEnvelope
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Data) > 0 {
		payload = wrapped.Data
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance parser: invalid JSON payload")
	}
	var event string
	if rawEvent, ok := obj["e"]; ok {
		if err := json.Unmarshal(rawEvent, &event); err != nil {
			return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance parser: invalid event type")
		}
	}

	switch event {
	case "aggTrade":
		return parseAggTrade(payload, recvAt)
	case "depthUpdate":
		return parseDepthUpdate(payload, recvAt)
	default:
		return app.IngestRequest{}, true, nil
	}
}

func parseAggTrade(payload []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	var m aggTrade
	if err := json.Unmarshal(payload, &m); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance aggTrade: invalid payload")
	}

	price, err := strconv.ParseFloat(m.PriceRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance aggTrade: invalid price")
	}
	size, err := strconv.ParseFloat(m.QuantityRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance aggTrade: invalid quantity")
	}
	instrument := naming.CanonicalInstrument(m.Symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "binance aggTrade: symbol is empty")
	}
	tsExchange := m.TradeTimeMs
	if tsExchange <= 0 {
		tsExchange = m.EventTimeMs
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	side := "buy"
	if m.BuyerIsMaker {
		side = "sell"
	}

	return app.IngestRequest{
		Venue:      VenueBinance,
		Instrument: instrument,
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: tsExchange,
		Payload: domain.TradeTickV1{
			Price:     price,
			Size:      size,
			Side:      side,
			TradeID:   fmt.Sprintf("%d", m.AggTradeID),
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseDepthUpdate(payload []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	var m depthUpdate
	if err := json.Unmarshal(payload, &m); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance depthUpdate: invalid payload")
	}

	instrument := naming.CanonicalInstrument(m.Symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "binance depthUpdate: symbol is empty")
	}

	bids, p := parseLevels(m.BidsRaw)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	asks, p := parseLevels(m.AsksRaw)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange := m.EventTimeMs
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	return app.IngestRequest{
		Venue:      VenueBinance,
		Instrument: instrument,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		Payload: domain.BookDeltaV1{
			Bids:      bids,
			Asks:      asks,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseLevels(raw [][]string) ([]domain.PriceLevel, *problem.Problem) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]domain.PriceLevel, 0, len(raw))
	for _, pair := range raw {
		if len(pair) < 2 {
			return nil, problem.New(problem.ValidationFailed, "binance depthUpdate: invalid level pair")
		}
		price, err := strconv.ParseFloat(pair[0], 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "binance depthUpdate: invalid level price")
		}
		size, err := strconv.ParseFloat(pair[1], 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "binance depthUpdate: invalid level size")
		}
		out = append(out, domain.PriceLevel{Price: price, Size: size})
	}
	return out, nil
}
