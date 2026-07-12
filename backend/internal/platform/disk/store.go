// Package disk implements document.FileStore on the local filesystem.
// Files are named by document id, so untrusted filenames never touch a path.
package disk

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Store struct {
	dir string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Open(_ context.Context, id string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(s.dir, id))
	if err != nil {
		return nil, fmt.Errorf("open stored file: %w", err)
	}
	return f, nil
}

func (s *Store) Save(_ context.Context, id string, r io.Reader) error {
	path := filepath.Join(s.dir, id)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(path)
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return fmt.Errorf("close file: %w", err)
	}
	return nil
}
