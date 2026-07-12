package document

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeRepo struct {
	docs    map[string]Document
	nextID  int
	updates []string // "id:status:errMsg" entries, to assert transitions
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{docs: map[string]Document{}}
}

func (r *fakeRepo) Create(_ context.Context, doc *Document) error {
	r.nextID++
	doc.ID = string(rune('a' + r.nextID - 1))
	r.docs[doc.ID] = *doc
	return nil
}

func (r *fakeRepo) Get(_ context.Context, id string) (Document, error) {
	doc, ok := r.docs[id]
	if !ok {
		return Document{}, ErrNotFound
	}
	return doc, nil
}

func (r *fakeRepo) List(_ context.Context) ([]Document, error) {
	out := make([]Document, 0, len(r.docs))
	for _, d := range r.docs {
		out = append(out, d)
	}
	return out, nil
}

func (r *fakeRepo) UpdateStatus(_ context.Context, id string, status Status, errMsg string) error {
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

type fakeStore struct {
	saved map[string]string
	err   error
}

func (s *fakeStore) Save(_ context.Context, id string, r io.Reader) error {
	if s.err != nil {
		return s.err
	}
	b, _ := io.ReadAll(r)
	if s.saved == nil {
		s.saved = map[string]string{}
	}
	s.saved[id] = string(b)
	return nil
}

func TestUploadQueuesDocumentAndStoresFile(t *testing.T) {
	repo := newFakeRepo()
	store := &fakeStore{}
	svc := NewService(repo, store)

	doc, err := svc.Upload(context.Background(), "report.pdf", "application/pdf", strings.NewReader("%PDF"))
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
	svc := NewService(newFakeRepo(), &fakeStore{})

	doc, err := svc.Upload(context.Background(), "notes.md", "application/octet-stream", strings.NewReader("# hi"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if doc.ContentType != "text/markdown" {
		t.Errorf("content type = %q, want text/markdown", doc.ContentType)
	}
}

func TestUploadRejectsUnsupportedType(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakeStore{})

	_, err := svc.Upload(context.Background(), "virus.exe", "application/x-msdownload", strings.NewReader("MZ"))
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("Upload() error = %v, want ErrUnsupportedType", err)
	}
	if len(repo.docs) != 0 {
		t.Error("document was persisted despite unsupported type")
	}
}

func TestUploadMarksFailedWhenFileSaveFails(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakeStore{err: errors.New("disk full")})

	_, err := svc.Upload(context.Background(), "report.pdf", "application/pdf", strings.NewReader("%PDF"))
	if err == nil {
		t.Fatal("Upload() error = nil, want save failure")
	}
	if len(repo.updates) != 1 || !strings.Contains(repo.updates[0], string(StatusFailed)) {
		t.Errorf("updates = %v, want one transition to failed", repo.updates)
	}
}
