package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"askdocs/backend/internal/document"
)

// Happy-path integration test against a real Postgres with the migrations
// applied. Skipped unless TEST_DATABASE_URL points at a DEDICATED test
// database — the queue assertions assume no other rows exist.
func TestDocumentLifecycleAgainstPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM documents`)
	})

	repo := NewDocumentRepository(pool)

	// Empty queue.
	if _, err := repo.ClaimNextQueued(ctx); !errors.Is(err, document.ErrNoneQueued) {
		t.Fatalf("ClaimNextQueued(empty) = %v, want ErrNoneQueued", err)
	}

	// Create → claim.
	doc := document.Document{Filename: "itest.pdf", ContentType: "application/pdf", Status: document.StatusQueued}
	if err := repo.Create(ctx, &doc); err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := repo.ClaimNextQueued(ctx)
	if err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}
	if claimed.ID != doc.ID || claimed.Status != document.StatusProcessing {
		t.Fatalf("claimed = %+v, want same doc as processing", claimed)
	}

	// A second claim finds nothing: the row is already processing.
	if _, err := repo.ClaimNextQueued(ctx); !errors.Is(err, document.ErrNoneQueued) {
		t.Fatalf("second claim = %v, want ErrNoneQueued", err)
	}

	// Save chunks with 384-dim vectors → ready.
	vec := make([]float32, 384)
	vec[0], vec[383] = 0.5, -0.5
	chunks := []document.Chunk{
		{DocumentID: doc.ID, Index: 0, Text: "primeiro trecho", Embedding: vec},
		{DocumentID: doc.ID, Index: 1, Text: "segundo trecho", Embedding: vec},
	}
	if err := repo.SaveChunks(ctx, doc.ID, chunks); err != nil {
		t.Fatalf("SaveChunks: %v", err)
	}

	got, err := repo.Get(ctx, doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != document.StatusReady {
		t.Errorf("status = %q, want ready", got.Status)
	}

	var count int
	var dims int
	if err := pool.QueryRow(ctx,
		`SELECT count(*), max(vector_dims(embedding)) FROM chunks WHERE document_id = $1`,
		doc.ID).Scan(&count, &dims); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if count != 2 || dims != 384 {
		t.Errorf("chunks = %d with dims %d, want 2 with 384", count, dims)
	}

	// SaveChunks is idempotent: saving again replaces, not duplicates.
	if err := repo.SaveChunks(ctx, doc.ID, chunks[:1]); err != nil {
		t.Fatalf("SaveChunks(again): %v", err)
	}
	pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE document_id = $1`, doc.ID).Scan(&count)
	if count != 1 {
		t.Errorf("chunks after re-save = %d, want 1 (replaced, not appended)", count)
	}
}
