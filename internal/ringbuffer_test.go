package internal

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_AddAndGet(t *testing.T) {
	ringBuffer := newRingBuffer[int](5)
	ringBuffer.add(1)
	ringBuffer.add(2)
	ringBuffer.add(3)

	expected := []int{1, 2, 3}
	actual := ringBuffer.get()
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, but got %v", expected, actual)
	}

	ringBuffer.add(4)
	ringBuffer.add(5)
	ringBuffer.add(6)

	expected = []int{2, 3, 4, 5, 6}
	actual = ringBuffer.get()
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, but got %v", expected, actual)
	}

	ringBuffer.add(7)
	ringBuffer.add(8)

	expected = []int{4, 5, 6, 7, 8}
	actual = ringBuffer.get()
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, but got %v", expected, actual)
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	ringBuffer := newRingBuffer[int](3)
	var wg sync.WaitGroup

	addValues := func(values []int) {
		for _, value := range values {
			ringBuffer.add(value)
			// Simulate delay
			time.Sleep(10 * time.Millisecond)
		}
		wg.Done()
	}

	readValues := func() {
		prices := ringBuffer.get()
		if len(prices) > 0 && len(prices) != ringBuffer.size {
			t.Errorf("Buffer length inconsistency: expected size %d but got %d", ringBuffer.size, len(prices))
		}
		wg.Done()
	}

	wg.Add(3)
	go addValues([]int{1, 2, 3})
	go addValues([]int{4, 5})
	go addValues([]int{6, 7, 8})

	wg.Add(2)
	go readValues()
	go readValues()

	wg.Wait()

	finalValues := ringBuffer.get()

	for _, value := range finalValues {
		if value < 1 || value > 8 {
			t.Errorf("Unexpected Value in buffer: %d", value)
		}
	}

	// Ensure the buffer size is consistent with expectations
	if len(finalValues) != ringBuffer.size {
		t.Errorf("Expected buffer size %d, but got %d", ringBuffer.size, len(finalValues))
	}
}
