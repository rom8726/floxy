package floxy

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Worker struct {
	engine   *Engine
	workerID string
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewWorker(engine *Engine, interval time.Duration) *Worker {
	return &Worker{
		engine:   engine,
		workerID: uuid.New().String(),
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log.Printf("Workflow worker %s started", w.workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Workflow worker %s stopping: context cancelled", w.workerID)

			return
		case <-w.stopCh:
			log.Printf("Workflow worker %s stopping: stop signal received", w.workerID)

			return
		case <-ticker.C:
			if _, err := w.processNext(ctx); err != nil {
				log.Printf("Workflow worker %s error: %v", w.workerID, err)
			}
		}
	}
}

func (w *Worker) Stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
}

func (w *Worker) processNext(ctx context.Context) (bool, error) {
	return w.engine.ExecuteNext(ctx, w.workerID)
}

type WorkerPool struct {
	workers []*Worker
	engine  *Engine
	mu      sync.Mutex
}

func NewWorkerPool(engine *Engine, size int, interval time.Duration) *WorkerPool {
	workers := make([]*Worker, size)
	for i := 0; i < size; i++ {
		workers[i] = NewWorker(engine, interval)
	}

	return &WorkerPool{
		workers: workers,
		engine:  engine,
	}
}

func (p *WorkerPool) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, worker := range p.workers {
		go worker.Start(ctx)
	}
}

func (p *WorkerPool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, worker := range p.workers {
		worker.Stop()
	}
}

func (p *WorkerPool) Shutdown(timeout time.Duration) error {
	p.Stop()

	return p.engine.Shutdown(timeout)
}

func (p *WorkerPool) Size() int {
	return len(p.workers)
}
