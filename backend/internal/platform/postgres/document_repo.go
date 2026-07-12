package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"askdocs/backend/internal/document"
)

// DocumentRepository implements document.Repository on Postgres.
type DocumentRepository struct {
	pool *pgxpool.Pool
}

func NewDocumentRepository(pool *pgxpool.Pool) *DocumentRepository {
	return &DocumentRepository{pool: pool}
}

func (r *DocumentRepository) Create(ctx context.Context, doc *document.Document) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO documents (filename, content_type, status)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at`,
		doc.Filename, doc.ContentType, doc.Status,
	).Scan(&doc.ID, &doc.CreatedAt, &doc.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (r *DocumentRepository) Get(ctx context.Context, id string) (document.Document, error) {
	var doc document.Document
	err := r.pool.QueryRow(ctx,
		`SELECT id, filename, content_type, status, error, created_at, updated_at
		 FROM documents WHERE id = $1`, id,
	).Scan(&doc.ID, &doc.Filename, &doc.ContentType, &doc.Status, &doc.Error, &doc.CreatedAt, &doc.UpdatedAt)
	if err != nil {
		if notFound(err) {
			return document.Document{}, document.ErrNotFound
		}
		return document.Document{}, fmt.Errorf("select document: %w", err)
	}
	return doc, nil
}

func (r *DocumentRepository) List(ctx context.Context) ([]document.Document, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, filename, content_type, status, error, created_at, updated_at
		 FROM documents ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("select documents: %w", err)
	}
	defer rows.Close()

	docs := []document.Document{}
	for rows.Next() {
		var doc document.Document
		if err := rows.Scan(&doc.ID, &doc.Filename, &doc.ContentType, &doc.Status, &doc.Error, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}
	return docs, nil
}

func (r *DocumentRepository) UpdateStatus(ctx context.Context, id string, status document.Status, errMsg string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE documents SET status = $2, error = $3, updated_at = now() WHERE id = $1`,
		id, status, errMsg)
	if err != nil {
		if notFound(err) {
			return document.ErrNotFound
		}
		return fmt.Errorf("update document status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return document.ErrNotFound
	}
	return nil
}

// ClaimNextQueued atomically flips the oldest queued document to processing.
// FOR UPDATE SKIP LOCKED lets concurrent workers (or API replicas) claim
// different rows without blocking each other.
func (r *DocumentRepository) ClaimNextQueued(ctx context.Context) (document.Document, error) {
	var doc document.Document
	err := r.pool.QueryRow(ctx,
		`UPDATE documents SET status = 'processing', updated_at = now()
		 WHERE id = (
		     SELECT id FROM documents
		     WHERE status = 'queued'
		     ORDER BY created_at
		     FOR UPDATE SKIP LOCKED
		     LIMIT 1
		 )
		 RETURNING id, filename, content_type, status, error, created_at, updated_at`,
	).Scan(&doc.ID, &doc.Filename, &doc.ContentType, &doc.Status, &doc.Error, &doc.CreatedAt, &doc.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return document.Document{}, document.ErrNoneQueued
		}
		return document.Document{}, fmt.Errorf("claim queued document: %w", err)
	}
	return doc, nil
}

// SaveChunks replaces the document's chunks and marks it ready in one
// transaction. The DELETE makes retries idempotent: a document reprocessed
// after a partial failure never ends up with duplicated chunks.
func (r *DocumentRepository) SaveChunks(ctx context.Context, documentID string, chunks []document.Chunk) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE document_id = $1`, documentID); err != nil {
		return fmt.Errorf("clear previous chunks: %w", err)
	}

	batch := &pgx.Batch{}
	for _, c := range chunks {
		batch.Queue(
			`INSERT INTO chunks (document_id, idx, text, embedding) VALUES ($1, $2, $3, $4::vector)`,
			documentID, c.Index, c.Text, vectorLiteral(c.Embedding))
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("insert chunks: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE documents SET status = 'ready', error = '', updated_at = now() WHERE id = $1`,
		documentID); err != nil {
		return fmt.Errorf("mark document ready: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit chunks: %w", err)
	}
	return nil
}

// notFound also covers ids that are not valid uuids: Postgres rejects them
// with invalid_text_representation (22P02), which the API must treat as 404.
func notFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
