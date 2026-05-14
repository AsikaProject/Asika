package consumer

import (
	"fmt"
	"log/slog"

	"asika/common/db"
	"asika/common/models"
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
	restarts int
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
	defer func() {
		if r := recover(); r != nil {
			slog.Error("writer actor panic recovered", "error", r, "restarts", w.restarts)
			if w.restarts < 3 {
				w.restarts++
				go w.run()
			}
		}
	}()
	for {
		select {
		case req := <-w.requests:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("write request panic recovered", "error", r)
						req.result <- fmt.Errorf("write panic: %v", r)
					}
				}()
				req.result <- db.PutPRWithIndex(req.key, req.value, req.prID, req.repoGroup, req.prNumber)
			}()
		case <-w.stop:
			slog.Info("writer actor stopped")
			return
		}
	}
}

// writeIssueLink stores an issue-PR link through the writer actor.
func (w *writerActor) writeIssueLink(link *models.IssuePRLink) error {
	return db.PutIssuePRLink(link)
}

// write submits a write request and waits for the result.
// Returns an error if the writer has been stopped.
func (w *writerActor) write(key string, value []byte, prID, repoGroup string, prNumber int) error {
	req := writeRequest{
		key:       key,
		value:     value,
		prID:      prID,
		repoGroup: repoGroup,
		prNumber:  prNumber,
		result:    make(chan error, 1),
	}
	select {
	case w.requests <- req:
		select {
		case err := <-req.result:
			return err
		case <-w.stop:
			return fmt.Errorf("writer actor stopped")
		}
	case <-w.stop:
		return fmt.Errorf("writer actor stopped")
	}
}

// Stop gracefully stops the writer goroutine.
func (w *writerActor) Stop() {
	close(w.stop)
}
