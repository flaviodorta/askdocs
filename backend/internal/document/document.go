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
}

// Repository persists documents. Implemented by platform/postgres.
type Repository interface {
	Create(ctx context.Context, doc *Document) error
	Get(ctx context.Context, id string) (Document, error)
	List(ctx context.Context) ([]Document, error)
	UpdateStatus(ctx context.Context, id string, status Status, errMsg string) error
}

// FileStore keeps the raw uploaded bytes until ingestion processes them.
// Implemented by platform/disk.
type FileStore interface {
	Save(ctx context.Context, id string, r io.Reader) error
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
