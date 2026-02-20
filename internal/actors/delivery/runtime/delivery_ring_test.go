package deliveryruntime

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

func makeEvt(seq int64) DeliveryEvent {
	return DeliveryEvent{
		Subject: domain.Subject{StreamType: "marketdata.trade", Venue: "binance", Symbol: "BTCUSDT"},
		Env:     envelope.Envelope{Seq: seq, Type: "marketdata.trade", Version: 1},
	}
}

func TestDeliveryRing_PushPopBasic(t *testing.T) {
	r := newDeliveryRing(4)
	if r.Len() != 0 {
		t.Fatalf("empty ring len=%d", r.Len())
	}
	r.PushBack(makeEvt(1))
	r.PushBack(makeEvt(2))
	r.PushBack(makeEvt(3))

	if r.Len() != 3 {
		t.Fatalf("len=%d want=3", r.Len())
	}

	evt, ok := r.PopFront()
	if !ok || evt.Env.Seq != 1 {
		t.Fatalf("pop got seq=%d ok=%v, want seq=1", evt.Env.Seq, ok)
	}
	evt, ok = r.PopFront()
	if !ok || evt.Env.Seq != 2 {
		t.Fatalf("pop got seq=%d ok=%v, want seq=2", evt.Env.Seq, ok)
	}
	if r.Len() != 1 {
		t.Fatalf("len=%d want=1", r.Len())
	}
}

func TestDeliveryRing_PopEmpty(t *testing.T) {
	r := newDeliveryRing(4)
	_, ok := r.PopFront()
	if ok {
		t.Fatal("pop from empty ring should return false")
	}
}

func TestDeliveryRing_FullWrap(t *testing.T) {
	r := newDeliveryRing(3)
	r.PushBack(makeEvt(1))
	r.PushBack(makeEvt(2))
	r.PushBack(makeEvt(3))

	if !r.IsFull() {
		t.Fatal("ring should be full")
	}

	// Pop one, push another — wraps around.
	r.PopFront()
	r.PushBack(makeEvt(4))

	if r.Len() != 3 {
		t.Fatalf("len=%d want=3", r.Len())
	}

	// Should get 2, 3, 4 in order.
	for _, want := range []int64{2, 3, 4} {
		evt, ok := r.PopFront()
		if !ok || evt.Env.Seq != want {
			t.Fatalf("pop got seq=%d ok=%v, want seq=%d", evt.Env.Seq, ok, want)
		}
	}
}

func TestDeliveryRing_DropFront(t *testing.T) {
	r := newDeliveryRing(4)
	r.PushBack(makeEvt(1))
	r.PushBack(makeEvt(2))
	r.DropFront()

	if r.Len() != 1 {
		t.Fatalf("len=%d want=1", r.Len())
	}
	evt, ok := r.PopFront()
	if !ok || evt.Env.Seq != 2 {
		t.Fatalf("after drop, pop got seq=%d want=2", evt.Env.Seq)
	}
}

func TestDeliveryRing_At(t *testing.T) {
	r := newDeliveryRing(4)
	r.PushBack(makeEvt(10))
	r.PushBack(makeEvt(20))
	r.PushBack(makeEvt(30))

	if r.At(0).Env.Seq != 10 {
		t.Fatalf("At(0)=%d want=10", r.At(0).Env.Seq)
	}
	if r.At(2).Env.Seq != 30 {
		t.Fatalf("At(2)=%d want=30", r.At(2).Env.Seq)
	}
}

func TestDeliveryRing_RemoveAt_Middle(t *testing.T) {
	r := newDeliveryRing(5)
	r.PushBack(makeEvt(1))
	r.PushBack(makeEvt(2))
	r.PushBack(makeEvt(3))
	r.PushBack(makeEvt(4))

	r.RemoveAt(1) // remove seq=2

	if r.Len() != 3 {
		t.Fatalf("len=%d want=3", r.Len())
	}

	// Should get 1, 3, 4.
	for _, want := range []int64{1, 3, 4} {
		evt, ok := r.PopFront()
		if !ok || evt.Env.Seq != want {
			t.Fatalf("pop got seq=%d want=%d", evt.Env.Seq, want)
		}
	}
}

func TestDeliveryRing_RemoveAt_Head(t *testing.T) {
	r := newDeliveryRing(4)
	r.PushBack(makeEvt(1))
	r.PushBack(makeEvt(2))
	r.RemoveAt(0)

	evt, ok := r.PopFront()
	if !ok || evt.Env.Seq != 2 {
		t.Fatalf("pop got seq=%d want=2", evt.Env.Seq)
	}
}

func TestDeliveryRing_RemoveAt_Wrapped(t *testing.T) {
	r := newDeliveryRing(4)
	// Fill and pop to advance head.
	r.PushBack(makeEvt(1))
	r.PushBack(makeEvt(2))
	r.PopFront() // head=1
	r.PopFront() // head=2, count=0

	// Now push 4 items wrapping around.
	r.PushBack(makeEvt(10)) // slot 2
	r.PushBack(makeEvt(20)) // slot 3
	r.PushBack(makeEvt(30)) // slot 0 (wrapped)
	r.PushBack(makeEvt(40)) // slot 1 (wrapped)

	r.RemoveAt(1) // remove seq=20

	if r.Len() != 3 {
		t.Fatalf("len=%d want=3", r.Len())
	}
	for _, want := range []int64{10, 30, 40} {
		evt, ok := r.PopFront()
		if !ok || evt.Env.Seq != want {
			t.Fatalf("pop got seq=%d want=%d", evt.Env.Seq, want)
		}
	}
}

func TestDeliveryRing_GCSlotCleared(t *testing.T) {
	r := newDeliveryRing(4)
	r.PushBack(makeEvt(1))
	r.PushBack(makeEvt(2))

	r.PopFront()

	// Slot 0 (the old head) should have been zeroed to allow GC.
	if r.buf[0].Env.Seq != 0 {
		t.Fatal("popped slot should be zeroed for GC")
	}
}
