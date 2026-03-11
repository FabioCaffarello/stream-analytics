package types

import (
	"marketmonkey/event"
	"time"
)

type Tick struct {
	Value int64
	T     time.Time
}

type Unixer interface {
	GetUnix() int64
}

type WSPayload struct {
	Pair      *event.Pair  `json:"pair"`
	Stream    event.Stream `json:"stream"`
	Timeframe int64        `json:"timeframe"`
	Data      []byte       `json:"data"`
}
