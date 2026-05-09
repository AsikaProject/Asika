package consumer

import (
	"log/slog"
	"sync"
)

// workerPool provides a fixed pool of goroutines for processing events.
// Each worker picks tasks from a shared channel, enabling parallel event
// processing while bounding resource usage.
type workerPool struct {
	tasks   chan func()
	stop    chan struct{}
	wg      sync.WaitGroup
	workers int
}

// newWorkerPool creates and starts a worker pool with the given size and buffer.
func newWorkerPool(size int) *workerPool {
	w := &workerPool{
		tasks:   make(chan func(), size*4),
		stop:    make(chan struct{}),
		workers: size,
	}
	for i := 0; i < size; i++ {
		w.wg.Add(1)
		go w.worker(i)
	}
	slog.Info("worker pool started", "workers", size, "buffer", size*4)
	return w
}

func (w *workerPool) worker(id int) {
	defer w.wg.Done()
	for {
		select {
		case task, ok := <-w.tasks:
			if !ok {
				return
			}
			task()
		case <-w.stop:
			for {
				select {
				case task, ok := <-w.tasks:
					if !ok {
						return
					}
					task()
				default:
					return
				}
			}
		}
	}
}

// Submit adds a task to the worker pool.
// Blocks if the task buffer is full (backpressure).
func (w *workerPool) Submit(task func()) {
	w.tasks <- task
}

// Stop gracefully shuts down the worker pool.
// Waits for all in-flight tasks to complete before returning.
func (w *workerPool) Stop() {
	close(w.stop)
	w.wg.Wait()
}
