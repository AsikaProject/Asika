package consumer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_SubmitAndExecute(t *testing.T) {
	p := newWorkerPool(2)
	defer p.Stop()

	var counter atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		p.Submit(func() {
			defer wg.Done()
			counter.Add(1)
		})
	}

	wg.Wait()
	if counter.Load() != 10 {
		t.Errorf("executed %d tasks, want 10", counter.Load())
	}
}

func TestWorkerPool_ConcurrentSubmit(t *testing.T) {
	p := newWorkerPool(4)
	defer p.Stop()

	var counter atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			p.Submit(func() {
				defer wg.Done()
				counter.Add(1)
			})
		}()
	}

	wg.Wait()
	if counter.Load() != 20 {
		t.Errorf("executed %d tasks, want 20", counter.Load())
	}
}

func TestWorkerPool_StopWaitsForCompletion(t *testing.T) {
	p := newWorkerPool(2)

	var counter atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		p.Submit(func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			counter.Add(1)
		})
	}

	p.Stop()
	wg.Wait()

	if counter.Load() != 5 {
		t.Errorf("executed %d tasks after Stop, want 5", counter.Load())
	}
}

func TestWorkerPool_BufferSize(t *testing.T) {
	p := newWorkerPool(2)

	if cap(p.tasks) != 8 {
		t.Errorf("expected buffer size 8, got %d", cap(p.tasks))
	}

	p.Stop()
}
