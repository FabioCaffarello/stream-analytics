package main

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
)

func TestFanOutEnvelopeStream_SignalDisabled(t *testing.T) {
	source := make(chan envelope.Envelope, 1)
	source <- envelope.Envelope{Type: "marketdata.trade", Seq: 1}
	close(source)

	aggregationCh, signalCh := fanOutEnvelopeStream(source, 8, false)
	if signalCh != nil {
		t.Fatal("signal channel must be nil when includeSignal=false")
	}

	select {
	case env, ok := <-aggregationCh:
		if !ok {
			t.Fatal("aggregation channel closed before first envelope")
		}
		if env.Seq != 1 {
			t.Fatalf("aggregation seq=%d want=1", env.Seq)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting aggregation envelope")
	}

	select {
	case _, ok := <-aggregationCh:
		if ok {
			t.Fatal("unexpected extra envelope")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting aggregation channel close")
	}
}
