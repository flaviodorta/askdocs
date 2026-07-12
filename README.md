# askdocs

Question-and-answer assistant over your own documents (RAG). Upload a PDF/text
file, ask a question in natural language, get an answer **with citations** to
the exact excerpts that support it.

## Architecture

Three services; Go is the central orchestrator:

| Service | Stack | Responsibility |
|---|---|---|
| `frontend/` | Next.js (TypeScript) | Upload + chat UI with source display. Talks only to the Go API. |
| `backend/` | Go (Hexagonal / Ports & Adapters) | HTTP API, auth, async ingestion pipeline. Talks to Python through ports. |
| `ai-service/` | Python (FastAPI) | Embeddings, retrieval, LLM generation. Stateless adapter. |

Data lives in **Postgres + pgvector** (documents, chunks, vectors).

**Embedding model**: `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`
(384 dimensions), served locally via fastembed/ONNX — multilingual (documents in
Portuguese work), no API key, no PyTorch. Configurable via `EMBEDDING_MODEL`;
changing it means a new pgvector migration if the dimension changes.

See [CLAUDE.md](CLAUDE.md) for the full architecture rules and
[ROADMAP.md](ROADMAP.md) for the build plan.

## Getting started

Prerequisites: Docker, Go 1.22+, Python 3.12+, Node 20+, and `pdftotext`
(poppler-utils) for PDF text extraction.

```bash
cp .env.example .env      # adjust if needed (defaults work locally)
make db-up                # Postgres+pgvector on localhost:5433, waits for healthy
make migrate-up           # apply migrations
```

## Commands

```bash
make db-up / db-down          # local infra
make migrate-up / migrate-down
make migrate-new name=foo     # new migration in backend/migrations

# Go backend (Phase 1+)
cd backend && go run ./cmd/api
cd backend && go test ./...

# Python AI service (one-time: python3 -m venv .venv && .venv/bin/pip install -e ".[dev]")
# Needs ANTHROPIC_API_KEY exported for /generate:  set -a; source ../.env; set +a
cd ai-service && .venv/bin/uvicorn app.main:app --reload
cd ai-service && .venv/bin/pytest

# Frontend (dev on http://localhost:3001 — port 3000 is often taken locally;
# /api/* is proxied to the Go API via Next rewrites, so no CORS setup needed)
cd frontend && npm run dev
```

## Status

Phase 0 (foundations) done. Next: Phase 1 — Go backend skeleton. See
[ROADMAP.md](ROADMAP.md).
