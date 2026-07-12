package document

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

type fakeExtractor struct {
	text string
	err  error
}

func (e fakeExtractor) Extract(_ context.Context, _ string, _ io.Reader) (string, error) {
	return e.text, e.err
}

type fakeEmbedder struct {
	err      error
	mismatch bool // return one vector too few
	block    bool // block until ctx is done (simulates shutdown mid-flight)

	mu    sync.Mutex
	calls int
}

func (e *fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	if e.err != nil {
		return nil, e.err
	}
	n := len(texts)
	if e.mismatch {
		n--
	}
	out := make([][]float32, n)
	for i := range out {
		out[i] = []float32{0.1, 0.2, 0.3}
	}
	return out, nil
}

// queuedDoc creates a queued document with raw bytes in the fake store.
func queuedDoc(t *testing.T, repo *fakeRepo, store *fakeStore) Document {
	t.Helper()
	doc := Document{Filename: "f.txt", ContentType: "text/plain", Status: StatusQueued}
	if err := repo.Create(context.Background(), &doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	store.Save(context.Background(), doc.ID, strings.NewReader("raw bytes"))
	return doc
}

func TestProcessHappyPath(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	// 2400 runes with chunk size 1000 / overlap 200 => 3 chunks
	longText := strings.TrimSpace(strings.Repeat("palavra ", 300))
	ing := NewIngestor(repo, store, fakeExtractor{text: longText}, &fakeEmbedder{})
	doc := queuedDoc(t, repo, store)

	if err := ing.Process(context.Background(), doc); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	chunks := repo.chunks[doc.ID]
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(chunks))
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("chunk[%d].Index = %d, want %d", i, c.Index, i)
		}
		if c.Text == "" || len(c.Embedding) == 0 {
			t.Errorf("chunk[%d] missing text or embedding", i)
		}
		if c.DocumentID != doc.ID {
			t.Errorf("chunk[%d].DocumentID = %q, want %q", i, c.DocumentID, doc.ID)
		}
	}
	if repo.status(doc.ID) != StatusReady {
		t.Errorf("status = %q, want ready", repo.status(doc.ID))
	}
}

func TestProcessFailsWhenFileMissing(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	store.openErr = errors.New("gone")
	ing := NewIngestor(repo, store, fakeExtractor{text: "x"}, &fakeEmbedder{})
	doc := queuedDoc(t, repo, store)

	if err := ing.Process(context.Background(), doc); err == nil || !strings.Contains(err.Error(), "open stored file") {
		t.Fatalf("Process() error = %v, want open stored file failure", err)
	}
}

func TestProcessFailsOnExtractError(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	ing := NewIngestor(repo, store, fakeExtractor{err: errors.New("corrupt pdf")}, &fakeEmbedder{})
	doc := queuedDoc(t, repo, store)

	if err := ing.Process(context.Background(), doc); err == nil || !strings.Contains(err.Error(), "extract text") {
		t.Fatalf("Process() error = %v, want extract failure", err)
	}
	if len(repo.chunks[doc.ID]) != 0 {
		t.Error("chunks saved despite extract failure")
	}
}

func TestProcessFailsWhenNoText(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	ing := NewIngestor(repo, store, fakeExtractor{text: "   \n "}, &fakeEmbedder{})
	doc := queuedDoc(t, repo, store)

	if err := ing.Process(context.Background(), doc); err == nil || !strings.Contains(err.Error(), "no extractable text") {
		t.Fatalf("Process() error = %v, want no extractable text", err)
	}
}

func TestProcessFailsOnEmbedError(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	ing := NewIngestor(repo, store, fakeExtractor{text: "some text"}, &fakeEmbedder{err: errors.New("ai down")})
	doc := queuedDoc(t, repo, store)

	if err := ing.Process(context.Background(), doc); err == nil || !strings.Contains(err.Error(), "embed chunks") {
		t.Fatalf("Process() error = %v, want embed failure", err)
	}
}

func TestProcessFailsOnEmbeddingCountMismatch(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	ing := NewIngestor(repo, store, fakeExtractor{text: "some text"}, &fakeEmbedder{mismatch: true})
	doc := queuedDoc(t, repo, store)

	if err := ing.Process(context.Background(), doc); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("Process() error = %v, want count mismatch", err)
	}
	if len(repo.chunks[doc.ID]) != 0 {
		t.Error("chunks saved despite mismatched embeddings")
	}
}
