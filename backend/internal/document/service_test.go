package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// fakeRepo is a concurrency-safe in-memory Repository shared by the package
// tests (service, ingestor and pool all mock the same ports).
type fakeRepo struct {
	mu      sync.Mutex
	docs    map[string]Document
	order   []string
	nextID  int
	updates []string // "id:status:errMsg", to assert transitions
	chunks  map[string][]Chunk
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{docs: map[string]Document{}, chunks: map[string][]Chunk{}}
}

func (r *fakeRepo) Create(_ context.Context, doc *Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	doc.ID = fmt.Sprintf("doc-%d", r.nextID)
	r.docs[doc.ID] = *doc
	r.order = append(r.order, doc.ID)
	return nil
}

func (r *fakeRepo) Get(_ context.Context, userID, id string) (Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.docs[id]
	if !ok || doc.UserID != userID {
		return Document{}, ErrNotFound
	}
	return doc, nil
}

func (r *fakeRepo) List(_ context.Context, userID string) ([]Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Document{}
	for _, id := range r.order {
		if r.docs[id].UserID == userID {
			out = append(out, r.docs[id])
		}
	}
	return out, nil
}

func (r *fakeRepo) UpdateStatus(_ context.Context, id string, status Status, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.docs[id]
	if !ok {
		return ErrNotFound
	}
	doc.Status = status
	doc.Error = errMsg
	r.docs[id] = doc
	r.updates = append(r.updates, id+":"+string(status)+":"+errMsg)
	return nil
}

func (r *fakeRepo) ClaimNextQueued(_ context.Context) (Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.order {
		if doc := r.docs[id]; doc.Status == StatusQueued {
			doc.Status = StatusProcessing
			r.docs[id] = doc
			return doc, nil
		}
	}
	return Document{}, ErrNoneQueued
}

func (r *fakeRepo) SaveChunks(_ context.Context, documentID string, chunks []Chunk) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.docs[documentID]
	if !ok {
		return ErrNotFound
	}
	r.chunks[documentID] = chunks
	doc.Status = StatusReady
	doc.Error = ""
	r.docs[documentID] = doc
	return nil
}

func (r *fakeRepo) status(id string) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.docs[id].Status
}

func (r *fakeRepo) countStatus(status Status) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, doc := range r.docs {
		if doc.Status == status {
			n++
		}
	}
	return n
}

type fakeStore struct {
	mu      sync.Mutex
	saved   map[string]string
	saveErr error
	openErr error
}

func newFakeStore() *fakeStore { return &fakeStore{saved: map[string]string{}} }

func (s *fakeStore) Save(_ context.Context, id string, r io.Reader) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	b, _ := io.ReadAll(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved[id] = string(b)
	return nil
}

func (s *fakeStore) Open(_ context.Context, id string) (io.ReadCloser, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.saved[id]
	if !ok {
		return nil, errors.New("file not found")
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func TestUploadQueuesDocumentAndStoresFile(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	svc := NewService(repo, store)

	doc, err := svc.Upload(context.Background(), "u1", "report.pdf", "application/pdf", strings.NewReader("%PDF"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if doc.Status != StatusQueued {
		t.Errorf("status = %q, want %q", doc.Status, StatusQueued)
	}
	if doc.ID == "" {
		t.Error("ID is empty, want repository-assigned id")
	}
	if store.saved[doc.ID] != "%PDF" {
		t.Errorf("stored bytes = %q, want raw upload under the document id", store.saved[doc.ID])
	}
}

func TestUploadFallsBackToExtension(t *testing.T) {
	svc := NewService(newFakeRepo(), newFakeStore())

	doc, err := svc.Upload(context.Background(), "u1", "notes.md", "application/octet-stream", strings.NewReader("# hi"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if doc.ContentType != "text/markdown" {
		t.Errorf("content type = %q, want text/markdown", doc.ContentType)
	}
}

func TestUploadRejectsUnsupportedType(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeStore())

	_, err := svc.Upload(context.Background(), "u1", "virus.exe", "application/x-msdownload", strings.NewReader("MZ"))
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("Upload() error = %v, want ErrUnsupportedType", err)
	}
	if len(repo.docs) != 0 {
		t.Error("document was persisted despite unsupported type")
	}
}

func TestUploadMarksFailedWhenFileSaveFails(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	store.saveErr = errors.New("disk full")
	svc := NewService(repo, store)

	_, err := svc.Upload(context.Background(), "u1", "report.pdf", "application/pdf", strings.NewReader("%PDF"))
	if err == nil {
		t.Fatal("Upload() error = nil, want save failure")
	}
	if len(repo.updates) != 1 || !strings.Contains(repo.updates[0], string(StatusFailed)) {
		t.Errorf("updates = %v, want one transition to failed", repo.updates)
	}
}

func TestRetryRequeuesFailedDocument(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeStore())

	doc := Document{UserID: "u1", Filename: "f.pdf", ContentType: "application/pdf", Status: StatusQueued}
	repo.Create(context.Background(), &doc)
	repo.UpdateStatus(context.Background(), doc.ID, StatusFailed, "boom")

	got, err := svc.Retry(context.Background(), "u1", doc.ID)
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if got.Status != StatusQueued || got.Error != "" {
		t.Errorf("document = %+v, want queued with cleared error", got)
	}
	if repo.status(doc.ID) != StatusQueued {
		t.Errorf("persisted status = %q, want queued", repo.status(doc.ID))
	}
}

func TestRetryRejectsNonFailedDocument(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeStore())

	doc := Document{UserID: "u1", Filename: "f.pdf", ContentType: "application/pdf", Status: StatusQueued}
	repo.Create(context.Background(), &doc)

	if _, err := svc.Retry(context.Background(), "u1", doc.ID); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("Retry() error = %v, want ErrNotRetryable", err)
	}
}

func TestDocumentsAreScopedPerUser(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeStore())

	docA, _ := svc.Upload(context.Background(), "user-a", "a.pdf", "application/pdf", strings.NewReader("%PDF"))
	repo.UpdateStatus(context.Background(), docA.ID, StatusFailed, "boom")

	if _, err := svc.Get(context.Background(), "user-b", docA.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user Get: error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Retry(context.Background(), "user-b", docA.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user Retry: error = %v, want ErrNotFound", err)
	}
	docs, _ := svc.List(context.Background(), "user-b")
	if len(docs) != 0 {
		t.Errorf("cross-user List = %d documents, want 0", len(docs))
	}
	own, _ := svc.List(context.Background(), "user-a")
	if len(own) != 1 {
		t.Errorf("owner List = %d documents, want 1", len(own))
	}
}
