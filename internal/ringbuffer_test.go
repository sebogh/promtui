package internal

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_AddAndGet(t *testing.T) {
	ringBuffer := NewRingBuffer[int](5)
	ringBuffer.Add(1)
	ringBuffer.Add(2)
	ringBuffer.Add(3)

	expected := []int{1, 2, 3}
	actual := ringBuffer.Get()
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, but got %v", expected, actual)
	}

	ringBuffer.Add(4)
	ringBuffer.Add(5)
	ringBuffer.Add(6)

	expected = []int{2, 3, 4, 5, 6}
	actual = ringBuffer.Get()
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, but got %v", expected, actual)
	}

	ringBuffer.Add(7)
	ringBuffer.Add(8)

	expected = []int{4, 5, 6, 7, 8}
	actual = ringBuffer.Get()
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, but got %v", expected, actual)
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	ringBuffer := NewRingBuffer[int](3)
	var wg sync.WaitGroup

	addValues := func(values []int) {
		for _, value := range values {
			ringBuffer.Add(value)
			// Simulate delay
			time.Sleep(10 * time.Millisecond)
		}
		wg.Done()
	}

	readValues := func() {
		prices := ringBuffer.Get()
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

	finalValues := ringBuffer.Get()

	for _, value := range finalValues {
		if value < 1 || value > 8 {
			t.Errorf("Unexpected value in buffer: %d", value)
		}
	}

	// Ensure the buffer size is consistent with expectations
	if len(finalValues) != ringBuffer.size {
		t.Errorf("Expected buffer size %d, but got %d", ringBuffer.size, len(finalValues))
	}
}
