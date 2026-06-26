package policykit_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/policykit"
)

var (
	hotPathApplySingleEnvelope envelope.Envelope
	hotPathApplySingleKeep     bool
	hotPathApplyBatchSink      []envelope.Envelope
)

func BenchmarkHotPathPolicyKitApplySingle(b *testing.B) {
	applier := policykit.NewApplier(policykit.NewCategoryResolver())
	decision := policykit.Decision{
		Actions: []policykit.Action{{Type: policykit.ActionDegradeStride, Stride: 2}},
	}
	env := envelope.Envelope{
		Type:       "marketdata.bookdelta",
		Version:    1,
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Seq:        1,
		Payload:    []byte(`{"seq":1}`),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env.Seq = int64(i + 1)
		hotPathApplySingleEnvelope, hotPathApplySingleKeep = applier.ApplySingle(decision, env, policykit.ApplyHooks{})
	}
}

func BenchmarkHotPathPolicyKitApplyBatchLen1(b *testing.B) {
	applier := policykit.NewApplier(policykit.NewCategoryResolver())
	decision := policykit.Decision{
		Actions: []policykit.Action{{Type: policykit.ActionDegradeStride, Stride: 2}},
	}
	env := envelope.Envelope{
		Type:       "marketdata.bookdelta",
		Version:    1,
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Seq:        1,
		Payload:    []byte(`{"seq":1}`),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env.Seq = int64(i + 1)
		hotPathApplyBatchSink = applier.Apply(decision, []envelope.Envelope{env}, policykit.ApplyHooks{})
	}
}
