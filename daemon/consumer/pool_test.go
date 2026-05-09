package consumer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"asika/common/models"
)

func testPool(cfg models.WorkerPoolConfig) *workerPool {
	return newWorkerPool(cfg)
}

func TestWorkerPool_SubmitAndExecute(t *testing.T) {
	p := testPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 4, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"})
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
	p := testPool(models.WorkerPoolConfig{MinWorkers: 4, MaxWorkers: 8, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"})
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
	p := testPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 4, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"})

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
	p := testPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 4, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"})
	defer p.Stop()

	if cap(p.tasks) != 16 {
		t.Errorf("expected buffer size 16, got %d", cap(p.tasks))
	}
}

func TestWorkerPool_Metrics(t *testing.T) {
	p := testPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 4, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"})
	defer p.Stop()

	m := p.Metrics()
	if m["workers"].(int32) != 2 {
		t.Errorf("expected 2 workers, got %d", m["workers"])
	}
	if m["total_tasks"].(uint64) != 0 {
		t.Errorf("expected 0 total tasks, got %d", m["total_tasks"])
	}
	if m["utilization_pct"].(int32) != 0 {
		t.Errorf("expected 0 utilization, got %d", m["utilization_pct"])
	}
}

func TestWorkerPool_UpdateConfig(t *testing.T) {
	p := testPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 4, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"})
	defer p.Stop()

	p.UpdateConfig(models.WorkerPoolConfig{MinWorkers: 3, MaxWorkers: 10, ScaleUpPct: 60, ScaleDownPct: 20, CooldownSecs: 15, StatsInterval: "10s"})

	m := p.Metrics()
	if m["workers"].(int32) != 2 {
		t.Errorf("expected 2 workers (min not auto-expanded), got %d", m["workers"])
	}
}

func TestWorkerPool_DynamicScaleUp(t *testing.T) {
	p := testPool(models.WorkerPoolConfig{
		MinWorkers:   1,
		MaxWorkers:   4,
		ScaleUpPct:   25,
		ScaleDownPct: 10,
		CooldownSecs: 0,
		StatsInterval: "50ms",
	})
	defer p.Stop()

	blockCh := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 6; i++ {
		wg.Add(1)
		p.Submit(func() {
			defer wg.Done()
			<-blockCh
		})
	}

	time.Sleep(150 * time.Millisecond)

	m := p.Metrics()
	workers := m["workers"].(int32)
	if workers <= 1 {
		t.Errorf("expected workers > 1 after scale up, got %d", workers)
	}

	close(blockCh)
	wg.Wait()
}

func TestWorkerPool_DynamicScaleDown(t *testing.T) {
	p := testPool(models.WorkerPoolConfig{
		MinWorkers:   1,
		MaxWorkers:   4,
		ScaleUpPct:   50,
		ScaleDownPct: 10,
		CooldownSecs: 0,
		StatsInterval: "50ms",
	})
	defer p.Stop()

	time.Sleep(100 * time.Millisecond)

	m := p.Metrics()
	workers := m["workers"].(int32)
	if workers != 1 {
		t.Errorf("expected workers == 1 after scale down, got %d", workers)
	}
}

func TestWorkerPool_CooldownPreventsFlapping(t *testing.T) {
	p := testPool(models.WorkerPoolConfig{
		MinWorkers:   1,
		MaxWorkers:   4,
		ScaleUpPct:   50,
		ScaleDownPct: 10,
		CooldownSecs: 60,
		StatsInterval: "50ms",
	})
	defer p.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		p.Submit(func() {
			defer wg.Done()
			time.Sleep(200 * time.Millisecond)
		})
	}

	time.Sleep(150 * time.Millisecond)

	m := p.Metrics()
	workers := m["workers"].(int32)
	if workers != 1 {
		t.Errorf("expected workers == 1 (cooldown blocked scale up), got %d", workers)
	}

	wg.Wait()
}
