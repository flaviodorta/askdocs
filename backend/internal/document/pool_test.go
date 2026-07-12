package document

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func runPool(t *testing.T, pool *Pool) (cancel func()) {
	t.Helper()
	ctx, cancelCtx := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pool.Run(ctx)
		close(done)
	}()
	return func() {
		cancelCtx()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("pool did not stop after cancellation")
		}
	}
}

func TestPoolProcessesAllQueuedDocuments(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	for i := 0; i < 5; i++ {
		queuedDoc(t, repo, store)
	}
	ing := NewIngestor(repo, store, fakeExtractor{text: "conteúdo do documento"}, &fakeEmbedder{})
	pool := NewPool(ing, repo, 2, time.Millisecond, discardLogger())

	stop := runPool(t, pool)
	defer stop()

	waitUntil(t, "5 documents ready", func() bool { return repo.countStatus(StatusReady) == 5 })
}

func TestPoolMarksBrokenDocumentFailed(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	doc := queuedDoc(t, repo, store)
	ing := NewIngestor(repo, store, fakeExtractor{text: "texto"}, &fakeEmbedder{err: errors.New("ai down")})
	pool := NewPool(ing, repo, 1, time.Millisecond, discardLogger())

	stop := runPool(t, pool)
	defer stop()

	waitUntil(t, "document failed", func() bool { return repo.status(doc.ID) == StatusFailed })

	got, _ := repo.Get(context.Background(), doc.ID)
	if !strings.Contains(got.Error, "embed chunks") || !strings.Contains(got.Error, "ai down") {
		t.Errorf("error = %q, want embed failure message persisted", got.Error)
	}
}

func TestPoolRequeuesDocumentInterruptedByShutdown(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	doc := queuedDoc(t, repo, store)
	// The embedder blocks until ctx is cancelled: the worker is mid-flight
	// when shutdown arrives, and the document must go back to queued.
	ing := NewIngestor(repo, store, fakeExtractor{text: "texto"}, &fakeEmbedder{block: true})
	pool := NewPool(ing, repo, 1, time.Millisecond, discardLogger())

	stop := runPool(t, pool)

	waitUntil(t, "document claimed", func() bool { return repo.status(doc.ID) == StatusProcessing })
	stop()

	if got := repo.status(doc.ID); got != StatusQueued {
		t.Errorf("status after shutdown = %q, want queued (requeued, not failed)", got)
	}
}
