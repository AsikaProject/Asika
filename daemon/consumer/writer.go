package consumer

import (
	"fmt"
	"log/slog"
	"sync"

	"asika/common/db"
	"asika/common/models"
)

// writeRequest is a request to the writer goroutine
type writeRequest struct {
	key       string
	value     []byte
	link      *models.IssuePRLink
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
	stopOnce sync.Once
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
				return
			}
			// Give up: signal stop so callers waiting on req.result via the
			// stop channel unblock and propagate "writer actor stopped".
			slog.Error("writer actor exceeded restart limit, stopping permanently")
			select {
			case <-w.stop:
				// already closed
			default:
				close(w.stop)
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
				if req.link != nil {
					req.result <- db.PutIssuePRLink(req.link)
					return
				}
				if req.prID == "" && req.repoGroup == "" && req.prNumber == 0 {
					req.result <- db.Put(db.BucketSyncHistory, req.key, req.value)
				} else {
					req.result <- db.PutPRWithIndex(req.key, req.value, req.prID, req.repoGroup, req.prNumber)
				}
			}()
		case <-w.stop:
			slog.Info("writer actor stopped")
			return
		}
	}
}

// writeIssueLink stores an issue-PR link through the writer actor.
func (w *writerActor) writeIssueLink(link *models.IssuePRLink) error {
	req := writeRequest{
		link:   link,
		result: make(chan error, 1),
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

// writeSyncRecord writes a sync history record to BucketSyncHistory.
func (w *writerActor) writeSyncRecord(key string, value []byte) error {
	req := writeRequest{
		key:    key,
		value:  value,
		result: make(chan error, 1),
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
	w.stopOnce.Do(func() {
		close(w.stop)
	})
}
