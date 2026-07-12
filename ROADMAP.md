# ROADMAP — askdocs

Task roadmap to build the project from zero to a working MVP:
**upload a document → ask a question → get an answer with citations.**

Phases are ordered by dependency. Each phase has a "Done when" gate — do not
start the next phase until the gate passes. Check items off as they land.

---

## Phase 0 — Repository foundations

Goal: a repo where all three services can grow, with local infra running.

- [x] `git init`, `.gitignore` (Go, Node, Python), `.editorconfig`
- [x] Create the top-level layout: `backend/`, `ai-service/`, `frontend/`, `docker-compose.yml`
- [x] `docker-compose.yml` with Postgres + pgvector (`pgvector/pgvector:pg16`), volume, healthcheck — host port **5433** (5432 taken by a local Postgres)
- [x] `.env.example` with DB credentials, service ports, and the LLM API key placeholder
- [x] Migration tooling for `backend/migrations` — `migrate/migrate` docker image via `make migrate-up/down/new`; first migration enables the `vector` extension
- [x] README with the architecture summary and "how to run" (can start as a stub)

**Done when:** `docker compose up -d` brings Postgres up healthy and a migration can be applied and rolled back. ✅ Verified 2026-07-12.

---

## Phase 1 — Go backend skeleton

Goal: a Go service that boots, connects to Postgres, and shuts down cleanly. No business logic yet.

- [ ] `go mod init`, folder layout per CLAUDE.md (`cmd/api`, `internal/document`, `internal/query`, `internal/auth`, `internal/platform/...`)
- [ ] Config loading from env vars (port, DB URL, AI service URL) — plain struct, no config framework
- [ ] HTTP server with router, request logging middleware, and `GET /healthz`
- [ ] Postgres connection pool (`pgxpool`) wired in `cmd/api/main.go`
- [ ] Graceful shutdown: SIGINT/SIGTERM → cancel root `context` → drain server
- [ ] CI-ready checks: `go vet ./...`, `gofmt -l .`, `go test ./...` all pass

**Done when:** `go run ./cmd/api` serves `/healthz` (including a DB ping) and Ctrl+C shuts down without errors.

---

## Phase 2 — Document domain and upload endpoint

Goal: a document can be uploaded, persisted, and queued — nothing processes it yet.

- [ ] Migration: `documents` table (id, filename, content type, status: `queued|processing|ready|failed`, error message, timestamps) and `chunks` table (id, document_id, index, text, `vector` column — dimension per the chosen embedding model)
- [ ] `internal/document`: `Document` and `Chunk` entities; `DocumentRepository` port; ingestion queue port
- [ ] `platform/postgres`: implement `DocumentRepository`; Postgres-backed queue (status column + `SELECT ... FOR UPDATE SKIP LOCKED`) — no Redis
- [ ] `platform/httpapi`: `POST /documents` (multipart upload → validate type/size → persist as `queued` → respond `202` immediately), `GET /documents`, `GET /documents/{id}` (status)
- [ ] Store the raw file (local disk directory is fine for MVP; keep it behind a small port so it can move later)
- [ ] Domain tests with the repository port mocked

**Done when:** `curl -F file=@doc.pdf` returns `202` with an id, and `GET /documents/{id}` shows it `queued`.

---

## Phase 3 — Python AI service: embeddings

Goal: a FastAPI service that turns text into vectors. Stateless, no DB access.

- [ ] FastAPI scaffold (`app/main.py`), `pyproject.toml`/`requirements.txt`, `GET /healthz`
- [ ] Pick the embedding model and record the decision (provider API vs. local `sentence-transformers`) — the vector dimension here must match the Phase 2 migration
- [ ] `POST /embed`: Pydantic contract `{texts: [str]} → {embeddings: [[float]], model: str, dim: int}`, batched
- [ ] Error contract (structured JSON errors + proper status codes) — this is the boundary Go will code against
- [ ] `pytest` covering the contract (mock the model/provider)

**Done when:** `curl -X POST /embed` with two texts returns two vectors of the documented dimension.

---

## Phase 4 — Ingestion pipeline (Go worker pool)

Goal: queued documents get processed end to end into stored vectors.

- [ ] `internal/document`: define the `EmbeddingService` port (consumed here, per CLAUDE.md)
- [ ] `platform/aiclient`: HTTP client implementing `EmbeddingService` against `/embed` (timeouts, `context` propagation, error wrapping)
- [ ] Text extraction: PDF (e.g. `pdftotext` or a Go lib) and plain text/markdown, behind a small extractor port
- [ ] Chunking: simple fixed-size with overlap; pure function, unit-tested with edge cases (empty doc, huge doc, no sentence breaks)
- [ ] Worker pool: fixed N goroutines consuming the queue via channels; bounded buffer for backpressure; `context`-aware shutdown that finishes in-flight documents
- [ ] Status transitions `queued → processing → ready|failed` with the error message persisted on failure; failed docs can be retried
- [ ] Tests per stage (extractor, chunker, worker loop with mocked ports) + one happy-path integration test against real Postgres

**Done when:** uploading a real PDF ends with the document `ready` and its chunks + vectors visible in the `chunks` table.

---

## Phase 5 — Query flow (RAG) end to end

Goal: a question comes in through Go and an answer with citations comes out.

> **Decision to confirm before starting:** CLAUDE.md gives Python "retrieval",
> but Go's `platform/postgres` owns the `VectorStore`. Recommended split that
> honors both: Go embeds the question (via `/embed`), Go searches pgvector
> through its `VectorStore` port, and sends the top-k chunks to Python
> `/generate`. Python stays stateless (no DB). Confirm and record the choice
> here before implementing.

- [ ] `platform/postgres`: `VectorStore.Search` using pgvector similarity (`<=>`), with an appropriate index (start exact/flat; add HNSW/IVFFlat only if needed)
- [ ] Python `POST /generate`: contract `{question, chunks: [{id, document_id, text}]} → {answer, citations: [{chunk_id, document_id}]}`; prompt instructs the LLM to answer *only* from the provided chunks and to cite them; refuses gracefully when the chunks don't contain the answer
- [ ] `internal/query`: `Query`/`Conversation`/`Message` entities; `LLMService` port; the ask use-case (embed question → retrieve top-k → generate → persist message with citations)
- [ ] `platform/aiclient`: implement `LLMService` against `/generate`
- [ ] `platform/httpapi`: `POST /queries` (or `/conversations/{id}/messages`) returning answer + citations (chunk text, document name, position)
- [ ] Domain tests with mocked ports; Python contract tests with a mocked LLM

**Done when:** with one ingested document, `curl POST /queries` returns a correct answer whose citations point at real chunks of that document.

---

## Phase 6 — Next.js frontend

Goal: the whole loop usable from a browser.

- [ ] Scaffold: `create-next-app` with App Router, `strict: true`, lint configured
- [ ] Typed API client for the Go backend (fetch wrappers + shared response types) — the only place URLs live
- [ ] Upload page: file picker → `POST /documents` → document list with live status (poll `GET /documents` — no websockets in the MVP)
- [ ] Chat page: question input, message history, loading/error states
- [ ] **Citations UI (core requirement):** each answer renders its sources — document name + excerpt, expandable to the full chunk
- [ ] Server components by default; client components only for upload form, chat input, and status polling

**Done when:** a person with no curl knowledge can upload a PDF, watch it become ready, ask a question, and see the answer with its sources.

---

## Phase 7 — Authentication

Goal: documents and conversations belong to a user; the API is no longer anonymous.

- [ ] Migration: `users` table; add `user_id` to documents and conversations
- [ ] `internal/auth`: registration + login (bcrypt), session issuing (opaque session token in an httpOnly cookie is enough for the MVP — decide and record)
- [ ] Auth middleware in `httpapi`; every document/query handler scopes by the authenticated user
- [ ] Ownership enforced in the domain layer (a user can never read another user's documents/chunks/answers — including via retrieval!)
- [ ] Frontend: login/register pages, authenticated fetch, logout
- [ ] Tests: unauthorized access is rejected; retrieval never returns another user's chunks

**Done when:** two users each upload a document and neither can see or get answers from the other's content.

---

## Phase 8 — Hardening and polish

Goal: the MVP is demonstrable, documented, and honest about failures.

- [ ] One scripted end-to-end smoke test (`docker compose up` → upload → ask → assert citation)
- [ ] Failure-path review: AI service down (Go degrades with a clear error), LLM timeout, malformed PDF, oversized file
- [ ] Rate limiting / body-size limits on the Go API
- [ ] CI pipeline: Go (vet, fmt, test), Python (pytest, ruff), frontend (lint, build) on every push
- [ ] README finalized: architecture diagram, setup from scratch, one command per task
- [ ] Optional (only if wanted after everything above): SSE streaming of LLM answers, Python `/generate` → Go relay → frontend

**Done when:** a fresh clone reaches a working demo by following the README only.

---

## Explicitly out of scope for the MVP

Per CLAUDE.md, do not build unless explicitly requested: Redis, gRPC (HTTP is
fine), multi-file formats beyond PDF/text, document sharing, admin panels,
observability stacks, or any "for the future" abstraction.
