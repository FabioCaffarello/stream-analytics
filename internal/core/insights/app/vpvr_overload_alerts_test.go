package app

import "testing"

func TestEvaluateVPVRAlertThresholds_L3StuckByWindowCount(t *testing.T) {
	thr := VPVRAlertThresholds{
		L3StuckWindows:               3,
		DropWithoutCloseWindows:      2,
		LatencyBudgetMs:              25,
		LatencyBudgetExceededWindows: 2,
	}
	state := VPVRAlertState{}
	for i := 0; i < 2; i++ {
		res := EvaluateVPVRAlertThresholds(state, VPVRAlertSignals{
			OverloadLevel: VPVROverloadL3,
		}, thr)
		if res.L3Stuck {
			t.Fatalf("unexpected l3 alert at i=%d", i)
		}
		state = res.NextState
	}
	res := EvaluateVPVRAlertThresholds(state, VPVRAlertSignals{
		OverloadLevel: VPVROverloadL3,
	}, thr)
	if !res.L3Stuck {
		t.Fatal("expected l3 stuck alert")
	}
}

func TestEvaluateVPVRAlertThresholds_DropWithoutClose(t *testing.T) {
	thr := VPVRAlertThresholds{
		L3StuckWindows:               3,
		DropWithoutCloseWindows:      2,
		LatencyBudgetMs:              25,
		LatencyBudgetExceededWindows: 2,
	}
	state := VPVRAlertState{}
	s := []VPVRAlertSignals{
		{DropTotal: 1, WindowCloseTotal: 0},
		{DropTotal: 2, WindowCloseTotal: 0},
	}
	for i := range s {
		res := EvaluateVPVRAlertThresholds(state, s[i], thr)
		state = res.NextState
		if i == 0 && res.DropWithoutClose {
			t.Fatal("unexpected drop alert before threshold")
		}
		if i == 1 && !res.DropWithoutClose {
			t.Fatal("expected drop-without-close alert")
		}
	}
}

func TestEvaluateVPVRAlertThresholds_LatencyBudgetByWindows(t *testing.T) {
	thr := VPVRAlertThresholds{
		L3StuckWindows:               3,
		DropWithoutCloseWindows:      2,
		LatencyBudgetMs:              25,
		LatencyBudgetExceededWindows: 2,
	}
	state := VPVRAlertState{}
	first := EvaluateVPVRAlertThresholds(state, VPVRAlertSignals{
		ProcessingMs: 30,
	}, thr)
	if first.LatencyBudgetExceeded {
		t.Fatal("unexpected latency alert before threshold")
	}
	second := EvaluateVPVRAlertThresholds(first.NextState, VPVRAlertSignals{
		ProcessingMs: 35,
	}, thr)
	if !second.LatencyBudgetExceeded {
		t.Fatal("expected latency budget alert")
	}
}
