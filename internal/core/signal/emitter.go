package signal

import (
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	EventType    = "signal.event"
	EventVersion = int(marketmodel.SignalVersion)
)

type Emission struct {
	Tenant     string
	StreamKey  marketmodel.StreamKey
	Seq        int64
	Event      marketmodel.SignalEvent
	RuleName   string
	EvalSpanMs int64
}

type SignalEmitter interface {
	Emit(emission Emission) *problem.Problem
}
