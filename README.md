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

See [CLAUDE.md](CLAUDE.md) for the full architecture rules and
[ROADMAP.md](ROADMAP.md) for the build plan.

## Getting started

Prerequisites: Docker, Go 1.22+, Python 3.12+, Node 20+.

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

# Python AI service (Phase 3+)
cd ai-service && uvicorn app.main:app --reload

# Frontend (Phase 6+)
cd frontend && npm run dev
```

## Status

Phase 0 (foundations) done. Next: Phase 1 — Go backend skeleton. See
[ROADMAP.md](ROADMAP.md).
