// Package document is the ingestion domain: what a document is, how it enters
// the system, and the ports the adapters must satisfy.
package document

import (
	"context"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"time"
)

type Status string

const (
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
)

var (
	ErrNotFound        = errors.New("document not found")
	ErrUnsupportedType = errors.New("unsupported document type (accepted: pdf, plain text, markdown)")
	ErrNoneQueued      = errors.New("no queued documents")
	ErrNotRetryable    = errors.New("only failed documents can be retried")
)

type Document struct {
	ID          string
	Filename    string
	ContentType string
	Status      Status
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Chunk struct {
	ID         string
	DocumentID string
	Index      int
	Text       string
	Embedding  []float32
}

// Repository persists documents and doubles as the ingestion queue (rows are
// claimed by status). Implemented by platform/postgres.
type Repository interface {
	Create(ctx context.Context, doc *Document) error
	Get(ctx context.Context, id string) (Document, error)
	List(ctx context.Context) ([]Document, error)
	UpdateStatus(ctx context.Context, id string, status Status, errMsg string) error
	// ClaimNextQueued atomically moves the oldest queued document to
	// processing and returns it; ErrNoneQueued when the queue is empty.
	ClaimNextQueued(ctx context.Context) (Document, error)
	// SaveChunks replaces the document's chunks and marks it ready, in one
	// transaction (idempotent under retries).
	SaveChunks(ctx context.Context, documentID string, chunks []Chunk) error
}

// FileStore keeps the raw uploaded bytes until ingestion processes them.
// Implemented by platform/disk.
type FileStore interface {
	Save(ctx context.Context, id string, r io.Reader) error
	Open(ctx context.Context, id string) (io.ReadCloser, error)
}

// EmbeddingService turns texts into vectors, one per input, order preserved.
// Implemented by platform/aiclient, which calls the Python service.
type EmbeddingService interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// TextExtractor turns raw uploaded bytes into plain text.
// Implemented by platform/extract.
type TextExtractor interface {
	Extract(ctx context.Context, contentType string, r io.Reader) (string, error)
}

var allowedContentTypes = map[string]bool{
	"application/pdf": true,
	"text/plain":      true,
	"text/markdown":   true,
}

var extensionContentTypes = map[string]string{
	".pdf": "application/pdf",
	".txt": "text/plain",
	".md":  "text/markdown",
}

// resolveContentType normalizes the declared content type, falling back to the
// file extension when the client sent nothing useful (curl and some browsers
// send application/octet-stream).
func resolveContentType(filename, declared string) (string, error) {
	ct := declared
	if parsed, _, err := mime.ParseMediaType(declared); err == nil {
		ct = parsed
	}
	if allowedContentTypes[ct] {
		return ct, nil
	}
	if ct == "" || ct == "application/octet-stream" {
		if byExt, ok := extensionContentTypes[filepath.Ext(filename)]; ok {
			return byExt, nil
		}
	}
	return "", ErrUnsupportedType
}
