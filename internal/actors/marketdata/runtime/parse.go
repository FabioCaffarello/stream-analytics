// Package mdruntime contains the MarketData subsystem actor, which bridges
// the ws.Consumer/Manager actor layer with the core marketdata ingest use case.
//
// Responsibilities:
//   - Spawn and supervise the ws.Manager child actor.
//   - Translate *ws.WsMessage into app.IngestRequest using a configurable ParseFunc.
//   - Forward *ws.WsError to the Guardian as runtime.ChildFailed.
//   - Log *ws.WsState transitions without spamming.
//
// Exchange-specific parsers (e.g., Binance, Kraken) live in separate adapter
// packages and are injected via SubsystemConfig.ParseMessage.
package mdruntime

import (
	"github.com/market-raccoon/internal/actors/marketdata/ws"
	"github.com/market-raccoon/internal/core/marketdata/app"
)

// ParseFunc converts a raw WebSocket message into an IngestRequest.
//
// Implementations should:
//   - Return skip=true for messages that carry no market data (e.g., heartbeat
//     acknowledgements, subscription confirmations, or parse failures that
//     should be silently discarded).
//   - Return skip=false and a populated IngestRequest for publishable events.
//
// ParseFunc is exchange-specific; concrete implementations live in adapter
// packages outside this module and are injected via SubsystemConfig.
type ParseFunc func(msg *ws.WsMessage) (req app.IngestRequest, skip bool)

// RawMessageV1 is a minimal pass-through payload used when no exchange-specific
// parser is available.  It wraps the raw wire bytes so that the ingest pipeline
// can still stamp, sequence and publish a trace envelope.
//
// EventType:  "marketdata.raw"
// Version:    1
type RawMessageV1 struct {
	Data []byte `json:"data"`
}

// MakeRawParseFunc returns a ParseFunc that wraps every received byte slice in
// a RawMessageV1 payload addressed to the given venue and instrument.
//
// Useful for:
//   - Integration tests that need a working end-to-end ingest without a real
//     exchange parser.
//   - Quick-start wiring where the full parser has not yet been implemented.
func MakeRawParseFunc(venue, instrument string) ParseFunc {
	return func(msg *ws.WsMessage) (app.IngestRequest, bool) {
		return app.IngestRequest{
			Venue:      venue,
			Instrument: instrument,
			EventType:  "marketdata.raw",
			Version:    1,
			TsExchange: msg.RecvAt.UnixMilli(),
			Payload:    RawMessageV1{Data: msg.Data},
		}, false
	}
}
