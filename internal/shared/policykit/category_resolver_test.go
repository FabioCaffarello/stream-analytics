package policykit

import "testing"

func TestCategoryResolverResolveSubject(t *testing.T) {
	resolver := NewCategoryResolver().
		WithSubject("custom.subject.v1/binance/BTCUSDT", CategoryTelemetry)

	tests := []struct {
		subject string
		want    Category
	}{
		{subject: "marketdata.bookdelta.v1.binance.BTCUSDT", want: CategoryDelta},
		{subject: "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m", want: CategorySnapshot},
		{subject: "insights.volume_profile_final.v1/binance/BTCUSDT/1m", want: CategoryCloseFinal},
		{subject: "runtime.telemetry.v1.global.platform", want: CategoryTelemetry},
		{subject: "custom.subject.v1/binance/BTCUSDT", want: CategoryTelemetry},
		{subject: "unknown.event.v1.binance.BTCUSDT", want: CategoryUnknown},
	}

	for _, tc := range tests {
		if got := resolver.ResolveSubject(tc.subject); got != tc.want {
			t.Fatalf("subject=%q got=%d want=%d", tc.subject, got, tc.want)
		}
	}
}

func TestNeverDropCloseFinalGuard(t *testing.T) {
	decision := Decision{Actions: []Action{{Type: ActionDropDelta}}}
	if !NeverDropCloseFinal(CategoryCloseFinal, decision) {
		t.Fatal("guard must block drop for close/final")
	}
	if NeverDropCloseFinal(CategoryDelta, decision) {
		t.Fatal("guard must not block drop for delta")
	}
}
