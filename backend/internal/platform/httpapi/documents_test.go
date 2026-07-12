package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"askdocs/backend/internal/document"
)

// In-memory implementations of the document ports, scoped per user like the
// real Postgres repository.

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

func (r *memRepo) Get(_ context.Context, userID, id string) (document.Document, error) {
	doc, ok := r.docs[id]
	if !ok || doc.UserID != userID {
		return document.Document{}, document.ErrNotFound
	}
	return doc, nil
}

func (r *memRepo) List(_ context.Context, userID string) ([]document.Document, error) {
	out := []document.Document{}
	for _, d := range r.docs {
		if d.UserID == userID {
			out = append(out, d)
		}
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

// upload sends a document as the given user and returns its response.
func (e *testEnv) upload(cookie *http.Cookie, filename, contentType, content string) documentResponse {
	e.t.Helper()
	body, ct := multipartBody(e.t, filename, contentType, content)
	rec := e.do(http.MethodPost, "/documents", body, ct, cookie)
	if rec.Code != http.StatusAccepted {
		e.t.Fatalf("upload: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp documentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		e.t.Fatalf("decode upload response: %v", err)
	}
	return resp
}

func TestUploadDocumentReturns202Queued(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	body, ct := multipartBody(t, "report.pdf", "application/pdf", "%PDF-1.4 fake")
	rec := env.do(http.MethodPost, "/documents", body, ct, cookie)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp documentResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID == "" || resp.Status != "queued" {
		t.Errorf("response = %+v, want non-empty id and status queued", resp)
	}
	if loc := rec.Header().Get("Location"); loc != "/documents/"+resp.ID {
		t.Errorf("Location = %q, want /documents/%s", loc, resp.ID)
	}
}

func TestUploadDocumentRejectsUnsupportedType(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	body, ct := multipartBody(t, "app.exe", "application/x-msdownload", "MZ")
	rec := env.do(http.MethodPost, "/documents", body, ct, cookie)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestUploadDocumentRequiresFileField(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	rec := env.do(http.MethodPost, "/documents", strings.NewReader("not multipart"), "text/plain", cookie)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetDocumentStatusAfterUpload(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")
	uploaded := env.upload(cookie, "notes.txt", "text/plain", "hello")

	rec := env.do(http.MethodGet, "/documents/"+uploaded.ID, nil, "", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got documentResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "queued" || got.Filename != "notes.txt" {
		t.Errorf("document = %+v, want queued notes.txt", got)
	}
}

func TestGetDocumentUnknownIDIs404(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	rec := env.do(http.MethodGet, "/documents/nope", nil, "", cookie)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDocumentsAreScopedPerUser(t *testing.T) {
	env := okEnv(t)
	alice := env.register("alice@example.com")
	bob := env.register("bob@example.com")
	uploaded := env.upload(alice, "segredo.pdf", "application/pdf", "%PDF")

	// Bob sees an empty list.
	rec := env.do(http.MethodGet, "/documents", nil, "", bob)
	if body := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || body != "[]" {
		t.Errorf("bob list = %d %s, want empty array", rec.Code, body)
	}

	// Bob cannot fetch or retry Alice's document — looks like it doesn't exist.
	if rec := env.do(http.MethodGet, "/documents/"+uploaded.ID, nil, "", bob); rec.Code != http.StatusNotFound {
		t.Errorf("bob get alice's doc: status = %d, want 404", rec.Code)
	}
	if rec := env.do(http.MethodPost, "/documents/"+uploaded.ID+"/retry", nil, "", bob); rec.Code != http.StatusNotFound {
		t.Errorf("bob retry alice's doc: status = %d, want 404", rec.Code)
	}

	// Alice still sees her document.
	rec = env.do(http.MethodGet, "/documents", nil, "", alice)
	var docs []documentResponse
	json.Unmarshal(rec.Body.Bytes(), &docs)
	if len(docs) != 1 || docs[0].Filename != "segredo.pdf" {
		t.Errorf("alice list = %+v, want her single document", docs)
	}
}

func TestRetryFailedDocument(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")
	uploaded := env.upload(cookie, "f.pdf", "application/pdf", "%PDF")
	env.docRepo.UpdateStatus(context.Background(), uploaded.ID, document.StatusFailed, "boom")

	rec := env.do(http.MethodPost, "/documents/"+uploaded.ID+"/retry", nil, "", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got documentResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "queued" || got.Error != "" {
		t.Errorf("document = %+v, want queued with cleared error", got)
	}
}

func TestRetryNonFailedDocumentIs409(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")
	uploaded := env.upload(cookie, "f.pdf", "application/pdf", "%PDF")

	rec := env.do(http.MethodPost, "/documents/"+uploaded.ID+"/retry", nil, "", cookie)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestListDocumentsIsEmptyArrayNotNull(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	rec := env.do(http.MethodGet, "/documents", nil, "", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}
