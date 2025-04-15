package internal

// See: https://medium.com/checker-engineering/a-practical-guide-to-implementing-a-generic-ring-buffer-in-go-866d27ec1a05

import (
	"sync"
)

type ringBuffer[T any] struct {
	buffer []T
	size   int
	mu     sync.Mutex
	write  int
	count  int
}

// newRingBuffer creates a new ring buffer with a fixed size.
func newRingBuffer[T any](size int) *ringBuffer[T] {
	return &ringBuffer[T]{
		buffer: make([]T, size),
		size:   size,
	}
}

// add inserts a new element into the buffer, overwriting the oldest if full.
func (rb *ringBuffer[T]) add(value T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buffer[rb.write] = value
	rb.write = (rb.write + 1) % rb.size

	if rb.count < rb.size {
		rb.count++
	}
}

// Get returns the contents of the buffer in FIFO order.
func (rb *ringBuffer[T]) get() []T {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	result := make([]T, 0, rb.count)

	for i := 0; i < rb.count; i++ {
		index := (rb.write + rb.size - rb.count + i) % rb.size
		result = append(result, rb.buffer[index])
	}

	return result
}
