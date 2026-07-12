# Askdocs

Question-and-answer assistant over your own documents (RAG). Upload a PDF/text
file, ask a question in natural language, get an answer **with citations** to
the exact excerpts that support it.

Every answer shows its sources — the filename and the exact excerpt that
supported it, expandable inline:

![Chat answering a question with an expanded citation](docs/screenshots/chat.png)

| Upload with live status | Private per-account access |
|---|---|
| ![Documents page with a ready badge](docs/screenshots/documents.png) | ![Login page](docs/screenshots/login.png) |

*(Screenshots taken from the running app; the assistant answers in the
question's language — here, Portuguese questions about an English contract.)*

## Architecture

Three services; Go is the central orchestrator. The frontend never talks to
Python or the database, and embeddings/LLM calls live exclusively in Python —
those boundaries are the project's core design rule (see [CLAUDE.md](CLAUDE.md)).

```mermaid
flowchart LR
    B([Browser])
    B -->|"/api/* rewrite (same origin, no CORS)"| F["frontend/<br/>Next.js UI · :3001"]
    F -->|REST + session cookie| G["backend/<br/>Go API · hexagonal · :8080"]
    G -->|"POST /embed · POST /generate"| P["ai-service/<br/>FastAPI · :8000"]
    G -->|"SQL + cosine search"| DB[("Postgres + pgvector · :5433<br/>users · documents · chunks<br/>vectors · conversations")]
    P -.->|Anthropic SDK| LLM["Claude<br/>(structured output)"]
```

| Service | Stack | Responsibility |
|---|---|---|
| `frontend/` | Next.js (TypeScript) | Upload + chat UI with source display. Talks only to the Go API. |
| `backend/` | Go (Hexagonal / Ports & Adapters) | HTTP API, auth, async ingestion pipeline, pgvector retrieval. Talks to Python through ports. |
| `ai-service/` | Python (FastAPI) | Embeddings and LLM generation. Stateless — no DB, no auth. |

### Ingestion: upload never blocks

Uploading answers `202` immediately; a Go worker pool processes the queue —
which is the `documents` table itself (status column + `FOR UPDATE SKIP
LOCKED`), so backpressure lives in Postgres, not in memory:

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser
    participant G as Go API
    participant W as Worker pool (Go)
    participant P as AI service
    participant DB as Postgres+pgvector

    B->>G: POST /documents (PDF)
    G->>DB: INSERT status=queued
    G-->>B: 202 Accepted {id}
    Note over B,G: UI polls status: queued → processing → ready
    W->>DB: claim next (FOR UPDATE SKIP LOCKED)
    W->>W: extract text (pdftotext) · chunk (1000/200, rune-safe)
    W->>P: POST /embed (batches)
    P-->>W: 384-dim vectors
    W->>DB: save chunks + embeddings · status=ready
```

Failures mark the document `failed` with the error persisted (visible in the
UI, with a retry button); a shutdown mid-processing requeues instead of failing.

### Asking: retrieval in Go, generation in Python

A recorded decision: Go owns retrieval (it embeds the question and searches
pgvector itself); Python stays stateless and only generates from the chunks Go
hands it.

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser
    participant G as Go API
    participant P as AI service
    participant DB as Postgres+pgvector

    B->>G: POST /queries {question}
    G->>P: POST /embed (question)
    P-->>G: question vector
    G->>DB: top-5 chunks (cosine <=>, only this user's ready docs)
    G->>P: POST /generate {question, chunks}
    P->>P: Claude, structured output {answer, citations}
    P-->>G: answer + cited chunk ids
    Note over G,P: cited ids validated against the retrieved set on BOTH sides —<br/>a hallucinated citation can never reach the UI
    G->>DB: persist user + assistant messages
    G-->>B: answer + citations (filename + snippet)
```

If retrieval finds zero chunks, Go short-circuits with a graceful "not found"
answer and never calls the LLM. Another user's chunks can never reach the
prompt: ownership is filtered in the SQL itself.

### Inside the Go backend (hexagonal)

Organized by domain, not by technical layer. Each domain package defines the
interfaces (ports) it **consumes**; concrete adapters in `platform/` implement
them, and everything is wired manually in `cmd/api/main.go` — no DI framework.

```
backend/
  cmd/api/            # entrypoint: manual dependency wiring
  internal/
    document/         # domain: Document, Chunk; ingestion pipeline, chunker
    query/            # domain: Conversation, Message, Citation; the Ask use-case
    auth/             # domain: users, sessions (bcrypt + hashed opaque tokens)
    platform/
      postgres/       # implements the repositories and VectorStore
      aiclient/       # implements EmbeddingService and LLMService (calls Python)
      extract/        # pdftotext + plain-text extraction
      httpapi/        # HTTP handlers, middleware (auth, rate limit, logging)
  migrations/         # SQL migrations (pgvector)
```

For example, `document` defines `EmbeddingService`; `aiclient` implements it.
The domain never knows *how* embeddings happen — swapping the AI service means
touching only the adapter.

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
