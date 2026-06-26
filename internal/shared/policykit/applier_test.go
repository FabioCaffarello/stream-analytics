package policykit

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
)

func TestApplierDeterministicSameInputSameOutput(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionDegradeStride, Stride: 2}}}
	envs := []envelope.Envelope{
		makeEnv("marketdata.bookdelta", 1),
		makeEnv("marketdata.bookdelta", 2),
		makeEnv("marketdata.bookdelta", 3),
		makeEnv("marketdata.bookdelta", 4),
	}
	a := NewApplier(NewCategoryResolver())
	b := NewApplier(NewCategoryResolver())

	outA := a.Apply(decision, envs, ApplyHooks{})
	outB := b.Apply(decision, envs, ApplyHooks{})
	if !sameSeq(outA, outB) {
		t.Fatalf("nondeterministic output: a=%v b=%v", seqs(outA), seqs(outB))
	}
}

func TestApplierNeverDropCloseFinal(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionDropDelta}}}
	envs := []envelope.Envelope{
		makeEnv("marketdata.bookdelta", 1),
		makeEnv("insights.volume_profile_final", 2),
	}
	out := NewApplier(NewCategoryResolver()).Apply(decision, envs, ApplyHooks{})
	if len(out) != 1 {
		t.Fatalf("len=%d want=1", len(out))
	}
	if out[0].Type != "insights.volume_profile_final" {
		t.Fatalf("type=%s want insights.volume_profile_final", out[0].Type)
	}
}

func TestApplierDropOnlyDelta(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionDropDelta}}}
	envs := []envelope.Envelope{
		makeEnv("marketdata.bookdelta", 1),
		makeEnv("insights.volume_profile_snapshot", 2),
	}
	out := NewApplier(NewCategoryResolver()).Apply(decision, envs, ApplyHooks{})
	if len(out) != 1 {
		t.Fatalf("len=%d want=1", len(out))
	}
	if out[0].Type != "insights.volume_profile_snapshot" {
		t.Fatalf("kept type=%s want insights.volume_profile_snapshot", out[0].Type)
	}
}

func TestApplierDegradeByStride(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionDegradeStride, Stride: 3}}}
	envs := []envelope.Envelope{
		makeEnv("insights.volume_profile_snapshot", 1),
		makeEnv("insights.volume_profile_snapshot", 2),
		makeEnv("insights.volume_profile_snapshot", 3),
		makeEnv("insights.volume_profile_snapshot", 4),
		makeEnv("insights.volume_profile_snapshot", 5),
		makeEnv("insights.volume_profile_snapshot", 6),
	}
	out := NewApplier(NewCategoryResolver()).Apply(decision, envs, ApplyHooks{})
	want := []int64{3, 6}
	if got := seqs(out); !slices.Equal(got, want) {
		t.Fatalf("seq=%v want=%v", got, want)
	}
}

func TestApplierDegradeStrideCloseFinalDoesNotShiftCadence(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionDegradeStride, Stride: 2}}}
	hooks := ApplyHooks{
		PartitionKey: func(env envelope.Envelope) string {
			return env.Venue + "|" + env.Instrument
		},
	}

	withClose := []envelope.Envelope{
		makeEnv("marketdata.bookdelta", 1),
		makeEnv("insights.volume_profile_final", 10),
		makeEnv("marketdata.bookdelta", 2),
		makeEnv("marketdata.bookdelta", 3),
		makeEnv("marketdata.bookdelta", 4),
	}
	withoutClose := []envelope.Envelope{
		makeEnv("marketdata.bookdelta", 1),
		makeEnv("marketdata.bookdelta", 2),
		makeEnv("marketdata.bookdelta", 3),
		makeEnv("marketdata.bookdelta", 4),
	}

	outWithClose := NewApplier(NewCategoryResolver()).Apply(decision, withClose, hooks)
	outWithoutClose := NewApplier(NewCategoryResolver()).Apply(decision, withoutClose, hooks)

	if !slices.ContainsFunc(outWithClose, func(env envelope.Envelope) bool {
		return env.Type == "insights.volume_profile_final" && env.Seq == 10
	}) {
		t.Fatal("close/final must always be emitted")
	}

	var deltaWithClose []int64
	for _, env := range outWithClose {
		if env.Type == "marketdata.bookdelta" {
			deltaWithClose = append(deltaWithClose, env.Seq)
		}
	}
	if got, want := deltaWithClose, seqs(outWithoutClose); !slices.Equal(got, want) {
		t.Fatalf("delta cadence shifted by close/final: got=%v want=%v", got, want)
	}
}

func TestApplierCompressNeverOnCloseFinal(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionCompressSnapshot}}}
	envs := []envelope.Envelope{
		makeEnv("insights.volume_profile_snapshot", 1),
		makeEnv("insights.volume_profile_final", 2),
	}
	out := NewApplier(NewCategoryResolver()).Apply(decision, envs, ApplyHooks{
		CompressSnapshot: func(env envelope.Envelope) (envelope.Envelope, bool) {
			if env.Meta == nil {
				env.Meta = map[string]string{}
			}
			env.Meta["compressed"] = "1"
			return env, true
		},
	})
	if out[0].Meta["compressed"] != "1" {
		t.Fatal("snapshot should be compressed")
	}
	if out[1].Meta["compressed"] != "" {
		t.Fatal("close/final must not be compressed")
	}
}

func TestApplierNoDuplicatesIntroduced(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionCompressSnapshot}}}
	envs := []envelope.Envelope{
		makeEnv("insights.volume_profile_snapshot", 10),
		makeEnv("insights.volume_profile_snapshot", 11),
	}
	out := NewApplier(NewCategoryResolver()).Apply(decision, envs, ApplyHooks{})
	seen := map[int64]bool{}
	for _, env := range out {
		if seen[env.Seq] {
			t.Fatalf("duplicate seq %d", env.Seq)
		}
		seen[env.Seq] = true
	}
}

func TestApplySingleEquivalentToApply(t *testing.T) {
	decision := Decision{
		Actions: []Action{
			{Type: ActionDropDelta},
			{Type: ActionDegradeStride, Stride: 2},
			{Type: ActionCompressSnapshot},
		},
	}
	envs := []envelope.Envelope{
		makeEnv("marketdata.bookdelta", 1),
		makeEnv("insights.volume_profile_snapshot", 2),
		makeEnv("insights.volume_profile_snapshot", 3),
		makeEnv("insights.volume_profile_final", 4),
	}
	hooks := ApplyHooks{
		CompressSnapshot: func(env envelope.Envelope) (envelope.Envelope, bool) {
			if env.Meta == nil {
				env.Meta = map[string]string{}
			}
			env.Meta["compressed"] = "1"
			return env, true
		},
	}

	batch := NewApplier(NewCategoryResolver()).Apply(decision, envs, hooks)
	singleApplier := NewApplier(NewCategoryResolver())
	single := make([]envelope.Envelope, 0, len(envs))
	for _, env := range envs {
		if envOut, keep := singleApplier.ApplySingle(decision, env, hooks); keep {
			single = append(single, envOut)
		}
	}

	if !reflect.DeepEqual(batch, single) {
		t.Fatalf("Apply vs ApplySingle mismatch: batch=%v single=%v", batch, single)
	}
}

func makeEnv(eventType string, seq int64) envelope.Envelope {
	return envelope.Envelope{
		Type:       eventType,
		Version:    1,
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Seq:        seq,
		TsIngest:   1000 + seq,
		Payload:    []byte(fmt.Sprintf(`{"seq":%d}`, seq)),
	}
}

func seqs(envs []envelope.Envelope) []int64 {
	out := make([]int64, 0, len(envs))
	for _, env := range envs {
		out = append(out, env.Seq)
	}
	return out
}

func sameSeq(a, b []envelope.Envelope) bool {
	return slices.Equal(seqs(a), seqs(b))
}
