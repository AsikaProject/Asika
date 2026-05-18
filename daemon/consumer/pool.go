package consumer

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"asika/common/models"
)

type poolMetrics struct {
	workers         atomic.Int32
	totalTasks      atomic.Uint64
	activeTasks     atomic.Int32
	utilization     atomic.Int32
	scaleUpEvents   atomic.Uint64
	scaleDownEvents atomic.Uint64
}

func (m *poolMetrics) snapshot() map[string]any {
	return map[string]any{
		"workers":         m.workers.Load(),
		"total_tasks":     m.totalTasks.Load(),
		"active_tasks":    m.activeTasks.Load(),
		"utilization_pct": m.utilization.Load(),
		"scale_ups":       m.scaleUpEvents.Load(),
		"scale_downs":     m.scaleDownEvents.Load(),
		"goroutines":      runtime.NumGoroutine(),
	}
}

type workerPool struct {
	tasks   chan func()
	stop    chan struct{}
	wg      sync.WaitGroup
	metrics *poolMetrics
	cfg     atomic.Value

	minWorkers   int
	maxWorkers   int
	scaleUpPct   int
	scaleDownPct int
	cooldown     time.Duration
	nextID       atomic.Int32

	mu         sync.Mutex
	cancels    []context.CancelFunc
	lastScaled time.Time
}

func newWorkerPool(cfg models.WorkerPoolConfig) *workerPool {
	w := &workerPool{
		tasks:        make(chan func(), cfg.MaxWorkers*4),
		stop:         make(chan struct{}),
		metrics:      &poolMetrics{},
		minWorkers:   cfg.MinWorkers,
		maxWorkers:   cfg.MaxWorkers,
		scaleUpPct:   cfg.ScaleUpPct,
		scaleDownPct: cfg.ScaleDownPct,
		cooldown:     time.Duration(cfg.CooldownSecs) * time.Second,
	}
	w.cfg.Store(cfg)

	for i := 0; i < cfg.MinWorkers; i++ {
		w.spawnWorker()
	}

	go w.adjustLoop()

	slog.Info("worker pool started", "min", cfg.MinWorkers, "max", cfg.MaxWorkers, "buffer", cfg.MaxWorkers*4)
	return w
}

func (w *workerPool) spawnWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	id := w.nextID.Add(1)
	w.metrics.workers.Add(1)

	w.mu.Lock()
	w.cancels = append(w.cancels, cancel)
	w.mu.Unlock()

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer w.metrics.workers.Add(-1)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				for {
					select {
					case task, ok := <-w.tasks:
						if !ok {
							return
						}
						w.exec(task)
					default:
						return
					}
				}
			case task, ok := <-w.tasks:
				if !ok {
					return
				}
				w.exec(task)
			}
		}
	}()
	slog.Info("worker spawned", "id", id, "total", w.metrics.workers.Load())
}

func (w *workerPool) exec(task func()) {
	w.metrics.activeTasks.Add(1)
	w.metrics.totalTasks.Add(1)
	task()
	w.metrics.activeTasks.Add(-1)
}

func (w *workerPool) Submit(task func()) {
	select {
	case w.tasks <- task:
	case <-w.stop:
		slog.Warn("worker pool: submit after stop, dropping task")
	}
}

func (w *workerPool) Stop() {
	close(w.stop)
	w.wg.Wait()
}

func (w *workerPool) Metrics() map[string]any {
	return w.metrics.snapshot()
}

func (w *workerPool) UpdateConfig(cfg models.WorkerPoolConfig) {
	w.mu.Lock()
	w.minWorkers = cfg.MinWorkers
	w.maxWorkers = cfg.MaxWorkers
	w.scaleUpPct = cfg.ScaleUpPct
	w.scaleDownPct = cfg.ScaleDownPct
	w.cooldown = time.Duration(cfg.CooldownSecs) * time.Second
	w.mu.Unlock()
	w.cfg.Store(cfg)
	slog.Info("worker pool config updated", "min", cfg.MinWorkers, "max", cfg.MaxWorkers)
}

func (w *workerPool) adjustLoop() {
	d := 30 * time.Second
	if cfg, ok := w.cfg.Load().(models.WorkerPoolConfig); ok {
		if parsed, err := time.ParseDuration(cfg.StatsInterval); err == nil && parsed > 0 {
			d = parsed
		}
	}
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.adjust()
		case <-w.stop:
			return
		}
	}
}

func (w *workerPool) adjust() {
	cap_ := cap(w.tasks)
	if cap_ == 0 {
		return
	}
	used := len(w.tasks)
	pct := (used * 100) / cap_
	w.metrics.utilization.Store(int32(pct))

	now := time.Now()
	currentWorkers := int(w.metrics.workers.Load())

	w.mu.Lock()
	canScale := now.Sub(w.lastScaled) >= w.cooldown
	scaleUp := w.scaleUpPct
	scaleDown := w.scaleDownPct
	maxW := w.maxWorkers
	minW := w.minWorkers
	w.mu.Unlock()

	if !canScale {
		return
	}

	if pct >= scaleUp && currentWorkers < maxW {
		w.mu.Lock()
		w.lastScaled = now
		w.mu.Unlock()
		w.spawnWorker()
		w.metrics.scaleUpEvents.Add(1)
		slog.Info("pool scale up", "workers", currentWorkers+1, "utilization_pct", pct)
		return
	}

	if pct <= scaleDown && currentWorkers > minW {
		w.mu.Lock()
		w.lastScaled = now
		if len(w.cancels) > 0 {
			w.cancels[len(w.cancels)-1]()
			w.cancels = w.cancels[:len(w.cancels)-1]
		}
		w.mu.Unlock()
		w.metrics.scaleDownEvents.Add(1)
		slog.Info("pool scale down", "workers", currentWorkers-1, "utilization_pct", pct)
	}
}
