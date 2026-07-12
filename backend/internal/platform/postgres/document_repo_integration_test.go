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
	// Wipe before AND after: an interrupted previous run must not poison this
	// one with leftover rows (users cascade to documents, chunks, conversations).
	if _, err := pool.Exec(ctx, `DELETE FROM users`); err != nil {
		t.Fatalf("clean test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM users`)
	})

	newUser := func(email string) string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`, email,
		).Scan(&id); err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		return id
	}
	alice := newUser("alice@itest.example")
	bob := newUser("bob@itest.example")

	repo := NewDocumentRepository(pool)

	// Empty queue.
	if _, err := repo.ClaimNextQueued(ctx); !errors.Is(err, document.ErrNoneQueued) {
		t.Fatalf("ClaimNextQueued(empty) = %v, want ErrNoneQueued", err)
	}

	// Create → claim.
	doc := document.Document{UserID: alice, Filename: "itest.pdf", ContentType: "application/pdf", Status: document.StatusQueued}
	if err := repo.Create(ctx, &doc); err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := repo.ClaimNextQueued(ctx)
	if err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}
	if claimed.ID != doc.ID || claimed.Status != document.StatusProcessing || claimed.UserID != alice {
		t.Fatalf("claimed = %+v, want alice's doc as processing", claimed)
	}

	// A second claim finds nothing: the row is already processing.
	if _, err := repo.ClaimNextQueued(ctx); !errors.Is(err, document.ErrNoneQueued) {
		t.Fatalf("second claim = %v, want ErrNoneQueued", err)
	}

	// Save chunks with 384-dim vectors → ready.
	vec := func(seed float32) []float32 {
		v := make([]float32, 384)
		v[0] = seed
		return v
	}
	if err := repo.SaveChunks(ctx, doc.ID, []document.Chunk{
		{DocumentID: doc.ID, Index: 0, Text: "trecho da alice", Embedding: vec(0.9)},
		{DocumentID: doc.ID, Index: 1, Text: "segundo trecho da alice", Embedding: vec(0.8)},
	}); err != nil {
		t.Fatalf("SaveChunks: %v", err)
	}

	got, err := repo.Get(ctx, alice, doc.ID)
	if err != nil || got.Status != document.StatusReady {
		t.Fatalf("Get(alice) = %+v, %v — want ready", got, err)
	}

	// Ownership: bob cannot see alice's document at all.
	if _, err := repo.Get(ctx, bob, doc.ID); !errors.Is(err, document.ErrNotFound) {
		t.Fatalf("Get(bob, alice's doc) = %v, want ErrNotFound", err)
	}
	bobDocs, _ := repo.List(ctx, bob)
	if len(bobDocs) != 0 {
		t.Fatalf("List(bob) = %d docs, want 0", len(bobDocs))
	}

	// Retrieval isolation: give bob his own ready document, then confirm each
	// user's vector search only ever returns their own chunks.
	bobDoc := document.Document{UserID: bob, Filename: "bob.pdf", ContentType: "application/pdf", Status: document.StatusQueued}
	if err := repo.Create(ctx, &bobDoc); err != nil {
		t.Fatalf("Create bob doc: %v", err)
	}
	if _, err := repo.ClaimNextQueued(ctx); err != nil {
		t.Fatalf("claim bob doc: %v", err)
	}
	if err := repo.SaveChunks(ctx, bobDoc.ID, []document.Chunk{
		{DocumentID: bobDoc.ID, Index: 0, Text: "trecho do bob", Embedding: vec(0.7)},
	}); err != nil {
		t.Fatalf("SaveChunks bob: %v", err)
	}

	vs := NewVectorStore(pool)
	for _, tc := range []struct {
		userID  string
		wantDoc string
	}{
		{alice, doc.ID},
		{bob, bobDoc.ID},
	} {
		chunks, err := vs.Search(ctx, tc.userID, vec(0.85), 10)
		if err != nil {
			t.Fatalf("Search(%s): %v", tc.userID, err)
		}
		if len(chunks) == 0 {
			t.Fatalf("Search(%s) returned nothing", tc.userID)
		}
		for _, c := range chunks {
			if c.DocumentID != tc.wantDoc {
				t.Fatalf("Search(%s) leaked chunk from document %s", tc.userID, c.DocumentID)
			}
		}
	}

	// SaveChunks is idempotent: saving again replaces, not duplicates.
	if err := repo.SaveChunks(ctx, doc.ID, []document.Chunk{
		{DocumentID: doc.ID, Index: 0, Text: "único", Embedding: vec(0.5)},
	}); err != nil {
		t.Fatalf("SaveChunks(again): %v", err)
	}
	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE document_id = $1`, doc.ID).Scan(&count)
	if count != 1 {
		t.Errorf("chunks after re-save = %d, want 1 (replaced, not appended)", count)
	}
}
