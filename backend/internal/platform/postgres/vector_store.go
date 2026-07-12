package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"askdocs/backend/internal/query"
)

// VectorStore implements query.VectorStore with pgvector cosine distance.
// Exact (sequential) scan — fine at MVP scale; add an HNSW index when the
// chunks table grows enough to hurt.
type VectorStore struct {
	pool *pgxpool.Pool
}

func NewVectorStore(pool *pgxpool.Pool) *VectorStore {
	return &VectorStore{pool: pool}
}

func (s *VectorStore) Search(ctx context.Context, embedding []float32, limit int) ([]query.RetrievedChunk, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.document_id, d.filename, c.idx, c.text
		 FROM chunks c
		 JOIN documents d ON d.id = c.document_id
		 WHERE d.status = 'ready'
		 ORDER BY c.embedding <=> $1::vector
		 LIMIT $2`,
		vectorLiteral(embedding), limit)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()

	var chunks []query.RetrievedChunk
	for rows.Next() {
		var c query.RetrievedChunk
		if err := rows.Scan(&c.ChunkID, &c.DocumentID, &c.Filename, &c.Index, &c.Text); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunks: %w", err)
	}
	return chunks, nil
}
