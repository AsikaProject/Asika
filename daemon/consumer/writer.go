package consumer

import (
	"log/slog"

	"asika/common/db"
)

// writeRequest is a request to the writer goroutine
type writeRequest struct {
	key       string
	value     []byte
	prID      string
	repoGroup string
	prNumber  int
	result    chan error
}

// writerActor handles all bbolt writes through a single goroutine.
// bbolt serializes write transactions internally, so routing all writes
// through one goroutine eliminates contention and provides backpressure.
type writerActor struct {
	requests chan writeRequest
	stop     chan struct{}
}

// newWriterActor creates and starts a writer goroutine.
func newWriterActor(bufferSize int) *writerActor {
	w := &writerActor{
		requests: make(chan writeRequest, bufferSize),
		stop:     make(chan struct{}),
	}
	go w.run()
	slog.Info("writer actor started", "buffer_size", bufferSize)
	return w
}

func (w *writerActor) run() {
	for {
		select {
		case req := <-w.requests:
			req.result <- db.PutPRWithIndex(req.key, req.value, req.prID, req.repoGroup, req.prNumber)
		case <-w.stop:
			slog.Info("writer actor stopped")
			return
		}
	}
}

// write submits a write request and waits for the result.
func (w *writerActor) write(key string, value []byte, prID, repoGroup string, prNumber int) error {
	req := writeRequest{
		key:       key,
		value:     value,
		prID:      prID,
		repoGroup: repoGroup,
		prNumber:  prNumber,
		result:    make(chan error, 1),
	}
	w.requests <- req
	return <-req.result
}

// Stop gracefully stops the writer goroutine.
func (w *writerActor) Stop() {
	close(w.stop)
}
