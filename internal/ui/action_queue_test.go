package ui

import "testing"

func TestActionQueueWakesAndDrainsInOrder(t *testing.T) {
	queue := newActionQueue(4)
	var values []int
	wakes := 0
	queue.enqueue(func() { values = append(values, 1) }, func() { wakes++ })
	queue.enqueue(func() { values = append(values, 2) }, func() { wakes++ })
	if wakes != 2 || len(values) != 0 {
		t.Fatalf("before drain: wakes=%d values=%v", wakes, values)
	}
	queue.drain()
	if len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("drain order = %v", values)
	}
	queue.drain()
}
