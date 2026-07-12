package document

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Service holds the ingestion use cases. Adapters are injected in cmd/api.
type Service struct {
	repo  Repository
	files FileStore
}

func NewService(repo Repository, files FileStore) *Service {
	return &Service{repo: repo, files: files}
}

// Upload validates the file, records it as queued, and stores the raw bytes.
// Processing happens later, in the ingestion pipeline — this must stay fast.
func (s *Service) Upload(ctx context.Context, filename, contentType string, r io.Reader) (Document, error) {
	ct, err := resolveContentType(filename, contentType)
	if err != nil {
		return Document{}, err
	}

	doc := Document{Filename: filename, ContentType: ct, Status: StatusQueued}
	if err := s.repo.Create(ctx, &doc); err != nil {
		return Document{}, fmt.Errorf("create document: %w", err)
	}

	if err := s.files.Save(ctx, doc.ID, r); err != nil {
		saveErr := fmt.Errorf("save file: %w", err)
		if uerr := s.repo.UpdateStatus(ctx, doc.ID, StatusFailed, saveErr.Error()); uerr != nil {
			return Document{}, errors.Join(saveErr, fmt.Errorf("mark document failed: %w", uerr))
		}
		return Document{}, saveErr
	}
	return doc, nil
}

func (s *Service) Get(ctx context.Context, id string) (Document, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]Document, error) {
	return s.repo.List(ctx)
}
