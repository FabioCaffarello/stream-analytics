package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type deliveryContract struct {
	version         int
	ownerBC         string
	producerBC      string
	schemaAuthority string
}

var deliveryContracts = map[string]deliveryContract{
	"marketdata.trade":                    {version: 1, ownerBC: "marketdata", producerBC: "marketdata", schemaAuthority: "marketdata"},
	"marketdata.bookdelta":                {version: 1, ownerBC: "marketdata", producerBC: "marketdata", schemaAuthority: "marketdata"},
	"marketdata.markprice":                {version: 1, ownerBC: "marketdata", producerBC: "marketdata", schemaAuthority: "marketdata"},
	"marketdata.open_interest":            {version: 1, ownerBC: "marketdata", producerBC: "marketdata", schemaAuthority: "marketdata"},
	"marketdata.liquidation":              {version: 1, ownerBC: "marketdata", producerBC: "marketdata", schemaAuthority: "marketdata"},
	"insights.crossvenue.trade_snapshot":  {version: 1, ownerBC: "insights", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"insights.crossvenue.spread_signal":   {version: 1, ownerBC: "insights", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"insights.microstructure_evidence":    {version: 1, ownerBC: "insights", producerBC: "evidence", schemaAuthority: "evidence"},
	"liquidity.evidence":                  {version: 1, ownerBC: "liquidity", producerBC: "evidence", schemaAuthority: "evidence"},
	"insights.regime_evidence":            {version: 1, ownerBC: "insights", producerBC: "evidence", schemaAuthority: "evidence"},
	"signal.composite":                    {version: 1, ownerBC: "signal", producerBC: "signal", schemaAuthority: "signal"},
	"signal.event":                        {version: 1, ownerBC: "signal", producerBC: "signal", schemaAuthority: "signal"},
	"strategy.intent":                     {version: 1, ownerBC: "strategy", producerBC: "strategy", schemaAuthority: "strategy"},
	"execution.event":                     {version: 1, ownerBC: "execution", producerBC: "execution", schemaAuthority: "execution"},
	"portfolio.state":                     {version: 1, ownerBC: "portfolio", producerBC: "portfolio", schemaAuthority: "portfolio"},
	"aggregation.snapshot":                {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.orderbook_inconsistency": {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.candle":                  {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.stats":                   {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.tape":                    {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.oi":                      {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.delta_volume":            {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.cvd":                     {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"aggregation.bar_stats":               {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"insights.heatmap_snapshot":           {version: 1, ownerBC: "insights", producerBC: "aggregation", schemaAuthority: "aggregation"},
	"insights.volume_profile_snapshot":    {version: 1, ownerBC: "insights", producerBC: "aggregation", schemaAuthority: "aggregation"},
}

// ValidateEnvelopeForDelivery enforces allowed delivery stream types and
// governance constraints used by the WS router.
func ValidateEnvelopeForDelivery(env envelope.Envelope) *problem.Problem {
	eventType := strings.ToLower(strings.TrimSpace(env.Type))
	contract, ok := deliveryContracts[eventType]
	if !ok {
		return problem.Newf(problem.ValidationFailed, "delivery stream type %q is not allowed", env.Type)
	}
	if env.Version != contract.version {
		return problem.Newf(
			problem.ValidationFailed,
			"delivery stream version mismatch for %q: got v%d want v%d",
			eventType,
			env.Version,
			contract.version,
		)
	}
	if contract.ownerBC != contract.producerBC {
		if strings.TrimSpace(contract.schemaAuthority) == "" {
			return problem.Newf(problem.ValidationFailed, "delivery governance missing schema authority for %q", eventType)
		}
		if contract.schemaAuthority != contract.ownerBC && contract.schemaAuthority != contract.producerBC {
			return problem.Newf(problem.ValidationFailed, "delivery governance invalid schema authority for %q", eventType)
		}
	}
	return nil
}
