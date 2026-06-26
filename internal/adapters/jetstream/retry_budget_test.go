package jetstream

import (
	"strings"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestApplyTransientRetryBudget_DeliveredAtLimitTerms(t *testing.T) {
	c := &Consumer{
		transientRetryBudget: 3,
		retryBudget:          newRetryBudgetTracker(4),
	}
	msg := nats.NewMsg("marketdata.trade.v1.binance.BTCUSDT")
	msg.Header.Set(jsHeaderNumDelivered, "3")

	decision := c.applyTransientRetryBudget(msg, envelope.Envelope{}, nil, transientDecision(ingestReasonQuarantinePublishError))
	if decision.Disposition != DispositionTerm {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionTerm)
	}
	if decision.ReasonCode != ingestReasonTransientExhausted {
		t.Fatalf("reason=%q want=%q", decision.ReasonCode, ingestReasonTransientExhausted)
	}
}

func TestApplyTransientRetryBudget_DeliveredBelowLimitKeepsNak(t *testing.T) {
	c := &Consumer{
		transientRetryBudget: 3,
		retryBudget:          newRetryBudgetTracker(4),
	}
	msg := nats.NewMsg("marketdata.trade.v1.binance.BTCUSDT")
	msg.Header.Set(jsHeaderNumDelivered, "2")

	decision := c.applyTransientRetryBudget(msg, envelope.Envelope{}, nil, transientDecision(ingestReasonQuarantinePublishError))
	if decision.Disposition != DispositionNak {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionNak)
	}
	if decision.ReasonCode != ingestReasonQuarantinePublishError {
		t.Fatalf("reason=%q want=%q", decision.ReasonCode, ingestReasonQuarantinePublishError)
	}
}

func TestApplyTransientRetryBudget_FallbackBoundedDropIsObservable(t *testing.T) {
	c := &Consumer{
		transientRetryBudget: 9,
		retryBudget:          newRetryBudgetTracker(2),
	}

	beforeDrop := testutil.ToFloat64(metrics.IngestDropTotal.WithLabelValues("buffer_full_drop"))
	msgs := []*nats.Msg{
		nats.NewMsg("marketdata.trade.v1.binance.BTCUSDT"),
		nats.NewMsg("marketdata.trade.v1.bybit.BTCUSDT"),
		nats.NewMsg("marketdata.trade.v1.okx.BTCUSDT"),
	}
	for _, msg := range msgs {
		decision := c.applyTransientRetryBudget(msg, envelope.Envelope{}, nil, transientDecision(ingestReasonTransientFailure))
		if decision.Disposition != DispositionNak {
			t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionNak)
		}
	}

	afterDrop := testutil.ToFloat64(metrics.IngestDropTotal.WithLabelValues("buffer_full_drop"))
	if afterDrop != beforeDrop+1 {
		t.Fatalf("buffer_full_drop delta=%f want=1", afterDrop-beforeDrop)
	}
	if got, want := c.retryBudget.size(), 2; got != want {
		t.Fatalf("fallback tracker size=%d want=%d", got, want)
	}
	desc := strings.ToLower(metrics.IngestDropTotal.WithLabelValues("buffer_full_drop").Desc().String())
	if strings.Contains(desc, "instrument") {
		t.Fatalf("ingest_drop_total must not expose instrument label: %s", desc)
	}
}
