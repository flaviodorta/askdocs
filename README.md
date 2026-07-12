# askdocs

Question-and-answer assistant over your own documents (RAG). Upload a PDF/text
file, ask a question in natural language, get an answer **with citations** to
the exact excerpts that support it.

## Architecture

Three services; Go is the central orchestrator. The frontend never talks to
Python or the database, and embeddings/LLM calls live exclusively in Python.

```
 browser
    │
    ▼
┌────────────────┐  /api/* rewrite   ┌──────────────────────────────┐
│  frontend/     │ ────────────────► │  backend/  (Go, hexagonal)   │
│  Next.js UI    │                   │  HTTP API · auth · ingestion │
└────────────────┘                   │  worker pool · retrieval     │
                                     └──────┬──────────────┬────────┘
                                 /embed     │              │ SQL + pgvector
                                 /generate  │              │
                                            ▼              ▼
                              ┌──────────────────┐  ┌───────────────────┐
                              │  ai-service/     │  │  Postgres         │
                              │  FastAPI         │  │  + pgvector       │
                              │  embeddings ·    │  │  documents ·      │
                              │  LLM generation  │  │  chunks · vectors │
                              └──────────────────┘  │  users · convos   │
                                                    └───────────────────┘
```

| Service | Stack | Responsibility |
|---|---|---|
| `frontend/` | Next.js (TypeScript) | Upload + chat UI with source display. Talks only to the Go API. |
| `backend/` | Go (Hexagonal / Ports & Adapters) | HTTP API, auth, async ingestion pipeline, pgvector retrieval. Talks to Python through ports. |
| `ai-service/` | Python (FastAPI) | Embeddings and LLM generation. Stateless — no DB, no auth. |

How a question is answered: Go embeds it via the Python `/embed`, searches
pgvector itself (cosine, only the user's own `ready` documents), sends the
top-5 chunks to Python `/generate`, and persists the answer with citations.
Cited chunk ids are validated against the retrieved set on both sides, so a
hallucinated citation can never reach the UI.

**Embedding model**: `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`
(384 dimensions), served locally via fastembed/ONNX — multilingual, no API key,
no PyTorch. Configurable via `EMBEDDING_MODEL`; changing it means a new pgvector
migration if the dimension changes.

**LLM**: `claude-opus-4-8` via the official Anthropic SDK with structured
output. Configurable via `LLM_MODEL`.

See [CLAUDE.md](CLAUDE.md) for the architecture rules and
[ROADMAP.md](ROADMAP.md) for how it was built.

## Setup from scratch

Prerequisites: Docker (with compose), Go 1.26+, Python 3.12+, Node 20+, `make`,
and `pdftotext` (Debian/Ubuntu: `sudo apt install poppler-utils`).

```bash
# 1. Environment — defaults work locally; add your Anthropic key for answers
cp .env.example .env
#    Set ANTHROPIC_API_KEY in .env (or run `ant auth login`). Without it,
#    everything works except answer generation, which returns a clear error.

# 2. Database
make db-up                # Postgres+pgvector on localhost:5433, waits for healthy
make migrate-up           # apply all migrations

# 3. AI service (terminal 1)
cd ai-service
python3 -m venv .venv && .venv/bin/pip install -e '.[dev]'
set -a; source ../.env; set +a
.venv/bin/uvicorn app.main:app --reload      # http://localhost:8000

# 4. Go API (terminal 2)
cd backend
set -a; source ../.env; set +a
go run ./cmd/api                             # http://localhost:8080

# 5. Frontend (terminal 3)
cd frontend
npm install
npm run dev                                  # http://localhost:3001
```

Open http://localhost:3001, create an account, upload a PDF, watch it become
**ready** (the first document downloads the embedding model — allow a minute),
then ask a question about it in the chat.

### One-command verification

```bash
./scripts/smoke.sh
```

Starts whatever isn't running (Postgres, migrations, both services), registers
a throwaway user, uploads a real PDF, waits for ingestion, asks a question and
asserts the answer cites that document. Without Anthropic credentials it instead
asserts the API degrades with a clear error and still passes; set
`SMOKE_REQUIRE_LLM=1` to make the LLM leg mandatory (as you would in a demo check).

## Commands

One command per task:

```bash
make db-up / db-down                   # local infra (data volume kept)
make migrate-up / migrate-down         # apply / roll back migrations
make migrate-new name=foo              # create a migration pair

cd backend && go run ./cmd/api         # run the API
cd backend && go test ./...            # all Go tests
cd backend && go vet ./... && gofmt -l .   # lint

cd ai-service && .venv/bin/uvicorn app.main:app --reload   # run the AI service
cd ai-service && .venv/bin/pytest      # Python tests
cd ai-service && .venv/bin/ruff check . && .venv/bin/ruff format --check .

cd frontend && npm run dev             # frontend dev server (port 3001)
cd frontend && npm run lint            # lint
cd frontend && npm run build           # production build

./scripts/smoke.sh                     # end-to-end smoke test
```

Integration tests against a real database are opt-in:
`TEST_DATABASE_URL=postgres://askdocs:askdocs@localhost:5433/askdocs_test?sslmode=disable go test ./...`
(create `askdocs_test` and run the migrations against it first).

CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) runs the same checks
on every push: Go (gofmt, vet, test), Python (ruff, pytest), frontend (lint, build).

## Configuration

All settings come from environment variables (see [.env.example](.env.example)):

| Variable | Default | Used by |
|---|---|---|
| `DATABASE_URL` | `postgres://askdocs:askdocs@localhost:5433/askdocs?sslmode=disable` | Go |
| `API_PORT` | `8080` | Go |
| `AI_SERVICE_URL` | `http://localhost:8000` | Go |
| `UPLOAD_DIR` | `./data/uploads` | Go |
| `INGEST_WORKERS` | `2` | Go |
| `EMBEDDING_MODEL` | `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` | Python |
| `ANTHROPIC_API_KEY` | — (required for answers) | Python |
| `LLM_MODEL` | `claude-opus-4-8` | Python |
| `POSTGRES_PORT` | `5433` (5432 is often taken locally) | docker compose |

## Failure behavior (by design)

- **AI service down** → ingestion marks the document `failed` with the error
  persisted and a retry button in the UI; asking returns a clear `502`.
- **No Anthropic credentials** → `/generate` returns `502` with an actionable
  message; retrieval, ingestion and history keep working.
- **LLM hangs** → Python times the call out at 90s (before Go's 120s client
  deadline) and reports "LLM did not answer within 90s".
- **Malformed PDF** → the document ends `failed` with the pdftotext error
  visible; `POST /documents/{id}/retry` requeues it.
- **Oversized upload** → bodies are capped (uploads 20 MiB, questions 8 KiB,
  auth 4 KiB) and rejected with a clear status.
- **Flooding** → per-IP rate limit (10 req/s, burst 30) answers `429`;
  `/healthz` is exempt.

## API at a glance

Auth is a session cookie (`POST /auth/register` or `/auth/login` sets it).
Everything except `/healthz` and `/auth/*` requires it.

| Endpoint | Purpose |
|---|---|
| `POST /documents` (multipart `file`) | upload → `202` + id, processed async |
| `GET /documents`, `GET /documents/{id}` | list / status |
| `POST /documents/{id}/retry` | requeue a failed document |
| `POST /queries` `{question, conversation_id?}` | ask → answer + citations |
| `GET /conversations/{id}` | conversation history |
