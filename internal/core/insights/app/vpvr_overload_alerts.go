package app

type VPVRAlertThresholds struct {
	L3StuckWindows               uint64
	DropWithoutCloseWindows      uint64
	LatencyBudgetMs              float64
	LatencyBudgetExceededWindows uint64
}

type VPVRAlertSignals struct {
	OverloadLevel    VPVROverloadLevel
	DropTotal        uint64
	WindowCloseTotal uint64
	ProcessingMs     float64
}

type VPVRAlertState struct {
	ConsecutiveL3            uint64
	ConsecutiveDropNoClose   uint64
	ConsecutiveLatencyBudget uint64
	LastDropTotal            uint64
	LastWindowCloseTotal     uint64
}

type VPVRAlertResult struct {
	NextState             VPVRAlertState
	L3Stuck               bool
	DropWithoutClose      bool
	LatencyBudgetExceeded bool
}

func EvaluateVPVRAlertThresholds(
	prev VPVRAlertState,
	signals VPVRAlertSignals,
	thresholds VPVRAlertThresholds,
) VPVRAlertResult {
	state := prev
	if signals.OverloadLevel == VPVROverloadL3 {
		state.ConsecutiveL3++
	} else {
		state.ConsecutiveL3 = 0
	}

	dropIncreased := signals.DropTotal > prev.LastDropTotal
	closeIncreased := signals.WindowCloseTotal > prev.LastWindowCloseTotal
	if dropIncreased && !closeIncreased {
		state.ConsecutiveDropNoClose++
	} else {
		state.ConsecutiveDropNoClose = 0
	}

	if signals.ProcessingMs > thresholds.LatencyBudgetMs {
		state.ConsecutiveLatencyBudget++
	} else {
		state.ConsecutiveLatencyBudget = 0
	}

	state.LastDropTotal = signals.DropTotal
	state.LastWindowCloseTotal = signals.WindowCloseTotal

	return VPVRAlertResult{
		NextState:             state,
		L3Stuck:               thresholds.L3StuckWindows > 0 && state.ConsecutiveL3 >= thresholds.L3StuckWindows,
		DropWithoutClose:      thresholds.DropWithoutCloseWindows > 0 && state.ConsecutiveDropNoClose >= thresholds.DropWithoutCloseWindows,
		LatencyBudgetExceeded: thresholds.LatencyBudgetExceededWindows > 0 && state.ConsecutiveLatencyBudget >= thresholds.LatencyBudgetExceededWindows,
	}
}
