package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"askdocs/backend/internal/document"
)

// In-memory implementations of the document ports, so handler tests run the
// real service without Postgres or disk.

type memRepo struct {
	docs   map[string]document.Document
	nextID int
}

func newMemRepo() *memRepo { return &memRepo{docs: map[string]document.Document{}} }

func (r *memRepo) Create(_ context.Context, doc *document.Document) error {
	r.nextID++
	doc.ID = fmt.Sprintf("doc-%d", r.nextID)
	doc.CreatedAt = time.Now()
	doc.UpdatedAt = doc.CreatedAt
	r.docs[doc.ID] = *doc
	return nil
}

func (r *memRepo) Get(_ context.Context, id string) (document.Document, error) {
	doc, ok := r.docs[id]
	if !ok {
		return document.Document{}, document.ErrNotFound
	}
	return doc, nil
}

func (r *memRepo) List(_ context.Context) ([]document.Document, error) {
	out := make([]document.Document, 0, len(r.docs))
	for _, d := range r.docs {
		out = append(out, d)
	}
	return out, nil
}

func (r *memRepo) UpdateStatus(_ context.Context, id string, status document.Status, errMsg string) error {
	doc, ok := r.docs[id]
	if !ok {
		return document.ErrNotFound
	}
	doc.Status = status
	doc.Error = errMsg
	r.docs[id] = doc
	return nil
}

func (r *memRepo) ClaimNextQueued(_ context.Context) (document.Document, error) {
	return document.Document{}, document.ErrNoneQueued
}

func (r *memRepo) SaveChunks(_ context.Context, _ string, _ []document.Chunk) error {
	return nil
}

type memStore struct{ saved int }

func (s *memStore) Save(_ context.Context, _ string, r io.Reader) error {
	io.Copy(io.Discard, r)
	s.saved++
	return nil
}

func (s *memStore) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func multipartBody(t *testing.T, filename, contentType, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename=%q`, filename)},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	part.Write([]byte(content))
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestUploadDocumentReturns202Queued(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	body, ct := multipartBody(t, "report.pdf", "application/pdf", "%PDF-1.4 fake")
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var resp documentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ID == "" || resp.Status != "queued" {
		t.Errorf("response = %+v, want non-empty id and status queued", resp)
	}
	if loc := rec.Header().Get("Location"); loc != "/documents/"+resp.ID {
		t.Errorf("Location = %q, want /documents/%s", loc, resp.ID)
	}
}

func TestUploadDocumentRejectsUnsupportedType(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	body, ct := multipartBody(t, "app.exe", "application/x-msdownload", "MZ")
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestUploadDocumentRequiresFileField(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	req := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "text/plain")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetDocumentStatusAfterUpload(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	body, ct := multipartBody(t, "notes.txt", "text/plain", "hello")
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var uploaded documentResponse
	json.Unmarshal(rec.Body.Bytes(), &uploaded)

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/"+uploaded.ID, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got documentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "queued" || got.Filename != "notes.txt" {
		t.Errorf("document = %+v, want queued notes.txt", got)
	}
}

func TestGetDocumentUnknownIDIs404(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRetryFailedDocument(t *testing.T) {
	repo := newMemRepo()
	svc := document.NewService(repo, &memStore{})
	srv := New(slog.New(slog.NewTextHandler(io.Discard, nil)), pingFunc(func(context.Context) error { return nil }), svc)

	doc := document.Document{Filename: "f.pdf", ContentType: "application/pdf", Status: document.StatusQueued}
	repo.Create(context.Background(), &doc)
	repo.UpdateStatus(context.Background(), doc.ID, document.StatusFailed, "boom")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/"+doc.ID+"/retry", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got documentResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "queued" || got.Error != "" {
		t.Errorf("document = %+v, want queued with cleared error", got)
	}
}

func TestRetryNonFailedDocumentIs409(t *testing.T) {
	repo := newMemRepo()
	svc := document.NewService(repo, &memStore{})
	srv := New(slog.New(slog.NewTextHandler(io.Discard, nil)), pingFunc(func(context.Context) error { return nil }), svc)

	doc := document.Document{Filename: "f.pdf", ContentType: "application/pdf", Status: document.StatusQueued}
	repo.Create(context.Background(), &doc)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/"+doc.ID+"/retry", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestListDocumentsIsEmptyArrayNotNull(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}
