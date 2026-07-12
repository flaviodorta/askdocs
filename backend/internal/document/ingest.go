package document

import (
	"context"
	"fmt"
)

// Chunking defaults: ~1000 runes keeps a chunk within typical embedding-model
// context comfortably; 200 of overlap preserves sentences cut at boundaries.
const (
	DefaultChunkSize    = 1000
	DefaultChunkOverlap = 200
)

// Ingestor runs the pipeline for one document:
// open file → extract text → chunk → embed → persist chunks + mark ready.
// Status bookkeeping on failure belongs to the caller (the worker pool), so
// each stage stays a pure "do or return error".
type Ingestor struct {
	repo     Repository
	files    FileStore
	extract  TextExtractor
	embedder EmbeddingService

	chunkSize    int
	chunkOverlap int
}

func NewIngestor(repo Repository, files FileStore, extract TextExtractor, embedder EmbeddingService) *Ingestor {
	return &Ingestor{
		repo:         repo,
		files:        files,
		extract:      extract,
		embedder:     embedder,
		chunkSize:    DefaultChunkSize,
		chunkOverlap: DefaultChunkOverlap,
	}
}

func (ing *Ingestor) Process(ctx context.Context, doc Document) error {
	file, err := ing.files.Open(ctx, doc.ID)
	if err != nil {
		return fmt.Errorf("open stored file: %w", err)
	}
	defer file.Close()

	text, err := ing.extract.Extract(ctx, doc.ContentType, file)
	if err != nil {
		return fmt.Errorf("extract text: %w", err)
	}

	parts := ChunkText(text, ing.chunkSize, ing.chunkOverlap)
	if len(parts) == 0 {
		return fmt.Errorf("document has no extractable text")
	}

	embeddings, err := ing.embedder.Embed(ctx, parts)
	if err != nil {
		return fmt.Errorf("embed chunks: %w", err)
	}
	if len(embeddings) != len(parts) {
		return fmt.Errorf("embedding count mismatch: %d texts, %d vectors", len(parts), len(embeddings))
	}

	chunks := make([]Chunk, len(parts))
	for i, part := range parts {
		chunks[i] = Chunk{
			DocumentID: doc.ID,
			Index:      i,
			Text:       part,
			Embedding:  embeddings[i],
		}
	}

	if err := ing.repo.SaveChunks(ctx, doc.ID, chunks); err != nil {
		return fmt.Errorf("save chunks: %w", err)
	}
	return nil
}
