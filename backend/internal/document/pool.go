package document

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Pool consumes the ingestion queue with a fixed number of workers.
//
// One dispatcher claims queued documents from the repository and hands them
// to workers through a bounded channel — when every worker is busy the send
// blocks, so the dispatcher stops claiming (backpressure: documents wait in
// Postgres as 'queued', not in memory as 'processing').
//
// Shutdown: on ctx cancellation the dispatcher stops claiming and closes the
// channel; workers finish the documents they already started. A document whose
// processing was cut by the cancellation is requeued, not marked failed.
type Pool struct {
	ingestor *Ingestor
	repo     Repository
	workers  int
	interval time.Duration // how long to sleep when the queue is empty
	logger   *slog.Logger
}

func NewPool(ingestor *Ingestor, repo Repository, workers int, interval time.Duration, logger *slog.Logger) *Pool {
	return &Pool{ingestor: ingestor, repo: repo, workers: workers, interval: interval, logger: logger}
}

// Run blocks until ctx is cancelled and every in-flight document is settled.
func (p *Pool) Run(ctx context.Context) {
	// Buffer 0: a claimed document is either in a worker's hands or not
	// claimed at all — nothing sits invisible in memory during a crash.
	ch := make(chan Document)

	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for doc := range ch {
				p.process(ctx, doc)
			}
		}()
	}

	p.dispatch(ctx, ch)
	close(ch)
	wg.Wait()
}

func (p *Pool) dispatch(ctx context.Context, ch chan<- Document) {
	for {
		if ctx.Err() != nil {
			return
		}

		doc, err := p.repo.ClaimNextQueued(ctx)
		switch {
		case errors.Is(err, ErrNoneQueued):
			p.sleep(ctx)
			continue
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			p.logger.Error("claim queued document", "error", err)
			p.sleep(ctx)
			continue
		}

		select {
		case ch <- doc:
		case <-ctx.Done():
			p.requeue(doc)
			return
		}
	}
}

func (p *Pool) process(ctx context.Context, doc Document) {
	err := p.ingestor.Process(ctx, doc)
	if err == nil {
		p.logger.Info("document ingested", "id", doc.ID, "filename", doc.Filename)
		return
	}

	// Interrupted by shutdown, not broken: put it back for the next run.
	if ctx.Err() != nil {
		p.requeue(doc)
		return
	}

	p.logger.Error("document ingestion failed", "id", doc.ID, "filename", doc.Filename, "error", err)
	if uerr := p.repo.UpdateStatus(context.WithoutCancel(ctx), doc.ID, StatusFailed, err.Error()); uerr != nil {
		p.logger.Error("mark document failed", "id", doc.ID, "error", uerr)
	}
}

// requeue runs on a fresh context: the pool's own context is already dead.
func (p *Pool) requeue(doc Document) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.repo.UpdateStatus(ctx, doc.ID, StatusQueued, ""); err != nil {
		p.logger.Error("requeue document on shutdown", "id", doc.ID, "error", err)
		return
	}
	p.logger.Info("document requeued on shutdown", "id", doc.ID)
}

func (p *Pool) sleep(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(p.interval):
	}
}
