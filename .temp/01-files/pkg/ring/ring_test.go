package ring

import (
	"testing"
)

type TestUnixValue struct {
	value int
	unix  int64
}

func (v TestUnixValue) GetUnix() int64 {
	return v.unix
}

func newTestDeque(capacity int) *Deque[TestUnixValue] {
	return NewDeque[TestUnixValue](capacity)
}

func TestPushBack(t *testing.T) {
	d := newTestDeque(5)
	vals := []TestUnixValue{
		{value: 1, unix: 1},
		{value: 2, unix: 2},
		{value: 3, unix: 3},
	}
	for _, v := range vals {
		d.PushBack(v)
	}
	if d.Len() != len(vals) {
		t.Errorf("Expected length %d, got %d", len(vals), d.Len())
	}
	if d.Front().value != 1 {
		t.Errorf("Expected front value 1, got %d", d.Front().value)
	}
	if d.Back().value != 3 {
		t.Errorf("Expected back value 3, got %d", d.Back().value)
	}
	// Check order via Get.
	for i, expected := range vals {
		got := d.Get(i)
		if got.value != expected.value {
			t.Errorf("At index %d, expected %d, got %d", i, expected.value, got.value)
		}
	}
}

func TestPushFront(t *testing.T) {
	d := newTestDeque(5)
	vals := []TestUnixValue{
		{value: 1, unix: 1},
		{value: 2, unix: 2},
		{value: 3, unix: 3},
	}
	for _, v := range vals {
		d.PushFront(v)
	}
	if d.Len() != len(vals) {
		t.Errorf("Expected length %d, got %d", len(vals), d.Len())
	}
	// Pushing to front reverses the order: expect 3,2,1.
	expectedOrder := []int{3, 2, 1}
	for i, expected := range expectedOrder {
		if d.Get(i).value != expected {
			t.Errorf("At index %d, expected %d, got %d", i, expected, d.Get(i).value)
		}
	}
}

func TestPopFront(t *testing.T) {
	d := newTestDeque(5)
	vals := []TestUnixValue{
		{value: 10, unix: 10},
		{value: 20, unix: 20},
		{value: 30, unix: 30},
	}
	for _, v := range vals {
		d.PushBack(v)
	}
	// PopFront should return items in order.
	for i, expected := range vals {
		got, ok := d.PopFront()
		if !ok {
			t.Errorf("PopFront failed at index %d", i)
		}
		if got.value != expected.value {
			t.Errorf("Expected %d, got %d", expected.value, got.value)
		}
	}
	// Deque is empty; further pop should fail.
	_, ok := d.PopFront()
	if ok {
		t.Error("Expected PopFront to fail on empty deque")
	}
}

func TestPopBack(t *testing.T) {
	d := newTestDeque(5)
	vals := []TestUnixValue{
		{value: 100, unix: 100},
		{value: 200, unix: 200},
		{value: 300, unix: 300},
	}
	for _, v := range vals {
		d.PushBack(v)
	}
	// PopBack should return items in reverse order.
	for i := len(vals) - 1; i >= 0; i-- {
		got, ok := d.PopBack()
		if !ok {
			t.Errorf("PopBack failed at index %d", i)
		}
		if got.value != vals[i].value {
			t.Errorf("Expected %d, got %d", vals[i].value, got.value)
		}
	}
	// Deque is empty; further pop should fail.
	_, ok := d.PopBack()
	if ok {
		t.Error("Expected PopBack to fail on empty deque")
	}
}

func TestGetAndGetRange(t *testing.T) {
	d := newTestDeque(10)
	vals := []TestUnixValue{
		{value: 1, unix: 1},
		{value: 2, unix: 2},
		{value: 3, unix: 3},
		{value: 4, unix: 4},
		{value: 5, unix: 5},
	}
	for _, v := range vals {
		d.PushBack(v)
	}
	// Test Get method.
	for i, expected := range vals {
		got := d.Get(i)
		if got.value != expected.value {
			t.Errorf("Get(%d): expected %d, got %d", i, expected.value, got.value)
		}
	}
	// Test GetRange: get indices 1 (inclusive) to 4 (exclusive): expect 2,3,4.
	rng := d.GetRange(1, 4)
	if len(rng) != 3 {
		t.Errorf("Expected range length 3, got %d", len(rng))
	}
	expectedRange := []int{2, 3, 4}
	for i, expected := range expectedRange {
		if rng[i].value != expected {
			t.Errorf("GetRange: at index %d, expected %d, got %d", i, expected, rng[i].value)
		}
	}
	// Test invalid index returns zero value.
	invalid := d.Get(10)
	if invalid.value != 0 {
		t.Error("Expected zero value for invalid index")
	}
}

func TestSetBack(t *testing.T) {
	d := newTestDeque(5)
	vals := []TestUnixValue{
		{value: 1, unix: 1},
		{value: 2, unix: 2},
	}
	for _, v := range vals {
		d.PushBack(v)
	}
	// Update the back element.
	newVal := TestUnixValue{value: 99, unix: 99}
	d.SetBack(newVal)
	if d.Back().value != 99 {
		t.Errorf("Expected back value to be 99, got %d", d.Back().value)
	}
}

func TestReset(t *testing.T) {
	d := newTestDeque(5)
	vals := []TestUnixValue{
		{value: 5, unix: 5},
		{value: 6, unix: 6},
	}
	for _, v := range vals {
		d.PushBack(v)
	}
	d.Reset()
	if d.Len() != 0 {
		t.Errorf("Expected length 0 after reset, got %d", d.Len())
	}
	zero := TestUnixValue{}
	if d.Front() != zero || d.Back() != zero {
		t.Error("Expected front and back to be zero values after reset")
	}
}

func TestPushRange(t *testing.T) {
	// Test pushing a range of older items.
	d := newTestDeque(10)
	initial := TestUnixValue{value: 50, unix: 50}
	d.PushBack(initial)
	older := []TestUnixValue{
		{value: 40, unix: 40},
		{value: 45, unix: 45},
	}
	result := d.PushRange(older)
	if result != -1 {
		t.Errorf("Expected PushRange to return -1 for older range, got %d", result)
	}
	// Expected order: 40,45,50.
	if d.Len() != 3 {
		t.Errorf("Expected length 3, got %d", d.Len())
	}
	expectedOrder := []int{40, 45, 50}
	for i, expected := range expectedOrder {
		if d.Get(i).value != expected {
			t.Errorf("At index %d, expected %d, got %d", i, expected, d.Get(i).value)
		}
	}

	// Test pushing a range of newer items.
	d.Reset()
	d.PushBack(initial)
	newer := []TestUnixValue{
		{value: 60, unix: 60},
		{value: 65, unix: 65},
	}
	result = d.PushRange(newer)
	if result != 1 {
		t.Errorf("Expected PushRange to return 1 for newer range, got %d", result)
	}
	// Expected order: 50, 60, 65.
	if d.Len() != 3 {
		t.Errorf("Expected length 3, got %d", d.Len())
	}
	expectedOrder = []int{50, 60, 65}
	for i, expected := range expectedOrder {
		if d.Get(i).value != expected {
			t.Errorf("At index %d, expected %d, got %d", i, expected, d.Get(i).value)
		}
	}

	// Test pushing a range that does not meet either condition.
	d.Reset()
	d.PushBack(TestUnixValue{value: 100, unix: 100})
	d.PushBack(TestUnixValue{value: 110, unix: 110})
	midRange := []TestUnixValue{
		{value: 105, unix: 105},
	}
	result = d.PushRange(midRange)
	if result != 0 {
		t.Errorf("Expected PushRange to return 0 for mid-range, got %d", result)
	}
	// Since midRange didn't satisfy either condition, the deque remains unchanged.
	if d.Len() != 2 {
		t.Errorf("Expected length 2, got %d", d.Len())
	}
}

func TestPushRangePartialOlder(t *testing.T) {
	d := newTestDeque(10)
	// Start with a single element.
	d.PushBack(TestUnixValue{value: 50, unix: 50})
	// Create a range with one qualifying (older) and one non-qualifying value.
	items := []TestUnixValue{
		{value: 40, unix: 40}, // qualifies: 40 < 50
		{value: 55, unix: 55}, // does not qualify: 55 is not < 50
	}
	result := d.PushRange(items)
	if result != -1 {
		t.Errorf("Expected PushRange to return -1 for older partial range, got %d", result)
	}
	// Expected deque: [40, 50]
	if d.Len() != 2 {
		t.Errorf("Expected length 2, got %d", d.Len())
	}
	if d.Front().value != 40 {
		t.Errorf("Expected front value 40, got %d", d.Front().value)
	}
	if d.Back().value != 50 {
		t.Errorf("Expected back value 50, got %d", d.Back().value)
	}
}

func TestPushRangePartialNewer(t *testing.T) {
	d := newTestDeque(10)
	// Start with a single element.
	d.PushBack(TestUnixValue{value: 50, unix: 50})
	// Create a range with one qualifying (newer) and one non-qualifying value.
	items := []TestUnixValue{
		{value: 55, unix: 55}, // qualifies: 55 > 50
		{value: 45, unix: 45}, // does not qualify: 45 is not > current back (which becomes 55 after push)
	}
	result := d.PushRange(items)
	if result != 1 {
		t.Errorf("Expected PushRange to return 1 for newer partial range, got %d", result)
	}
	// Expected deque: [50, 55]
	if d.Len() != 2 {
		t.Errorf("Expected length 2, got %d", d.Len())
	}
	if d.Front().value != 50 {
		t.Errorf("Expected front value 50, got %d", d.Front().value)
	}
	if d.Back().value != 55 {
		t.Errorf("Expected back value 55, got %d", d.Back().value)
	}
}
