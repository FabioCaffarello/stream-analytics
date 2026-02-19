package observability

import "time"

// BusObserver records bus publish/drop outcomes without coupling adapters to
// concrete metrics backends.
type BusObserver interface {
	IncPublished(eventType, venue string)
	IncDropped(subscriberIndex int)
	IncPublishError(kind string)
	ObservePublishLatency(busType string, latency time.Duration)
	IncConsumed(busType, status string)
	IncRedelivered(busType string)
	ObserveAckLatency(busType string, latency time.Duration)
	SetConsumerLag(busType string, lag int64)
}

type noopBusObserver struct{}

func (noopBusObserver) IncPublished(string, string)                 {}
func (noopBusObserver) IncDropped(int)                              {}
func (noopBusObserver) IncPublishError(string)                      {}
func (noopBusObserver) ObservePublishLatency(string, time.Duration) {}
func (noopBusObserver) IncConsumed(string, string)                  {}
func (noopBusObserver) IncRedelivered(string)                       {}
func (noopBusObserver) ObserveAckLatency(string, time.Duration)     {}
func (noopBusObserver) SetConsumerLag(string, int64)                {}

// NopBusObserver returns a no-op observer safe for production defaults/tests.
func NopBusObserver() BusObserver { return noopBusObserver{} }
