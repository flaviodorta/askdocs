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

- [x] `go mod init` (`askdocs/backend`), folder layout per CLAUDE.md (`cmd/api`, `internal/document`, `internal/query`, `internal/auth`, `internal/platform/...`)
- [x] Config loading from env vars (port, DB URL, AI service URL) — plain struct, no config framework
- [x] HTTP server with router (stdlib `ServeMux`, no router dep), request logging middleware, and `GET /healthz`
- [x] Postgres connection pool (`pgxpool`) wired in `cmd/api/main.go`
- [x] Graceful shutdown: SIGINT/SIGTERM → cancel root `context` → drain server
- [x] CI-ready checks: `go vet ./...`, `gofmt -l .`, `go test ./...` all pass

**Done when:** `go run ./cmd/api` serves `/healthz` (including a DB ping) and Ctrl+C shuts down without errors. ✅ Verified 2026-07-12 (SIGTERM → clean exit 0).

---

## Phase 2 — Document domain and upload endpoint

Goal: a document can be uploaded, persisted, and queued — nothing processes it yet.

- [x] Migration: `documents` table (id, filename, content type, status: `queued|processing|ready|failed`, error message, timestamps) and `chunks` table (id, document_id, idx, text) — the `vector` column was deferred to Phase 3/4, since its dimension depends on the embedding model chosen there
- [x] `internal/document`: `Document` and `Chunk` entities; `Repository` and `FileStore` ports; `Service.Upload` use case
- [x] `platform/postgres`: implement `Repository`; the queue is the `documents` table itself (status column) — the `SKIP LOCKED` dequeue ships with its consumer, the Phase 4 worker pool
- [x] `platform/httpapi`: `POST /documents` (multipart → validate type/size → persist as `queued` → `202` + Location), `GET /documents`, `GET /documents/{id}` (status)
- [x] Raw file stored on local disk (`platform/disk`), named by document id, behind the `FileStore` port
- [x] Domain tests with the repository port mocked (+ handler tests over in-memory ports)

**Done when:** `curl -F file=@doc.pdf` returns `202` with an id, and `GET /documents/{id}` shows it `queued`. ✅ Verified 2026-07-12 (also: 415 for unsupported type, 404 for unknown/invalid id, row in Postgres, raw bytes on disk).

---

## Phase 3 — Python AI service: embeddings

Goal: a FastAPI service that turns text into vectors. Stateless, no DB access.

- [x] FastAPI scaffold (`app/main.py`), `pyproject.toml`, `GET /healthz`
- [x] Model decision: `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` (**384 dims**) via fastembed/ONNX — multilingual (PT docs), no API key, no PyTorch; env-configurable (`EMBEDDING_MODEL`), isolated in `app/embeddings.py`. The Phase 4 migration must use `vector(384)`.
- [x] `POST /embed`: Pydantic contract `{texts: [str]} → {embeddings: [[float]], model: str, dim: int}`, batched
- [x] Error contract: FastAPI 422 validation JSON + `{"detail": ...}` on 500 (no stack leaks) — the boundary Go codes against
- [x] `pytest` covering the contract (embedder mocked via dependency override)

**Done when:** `curl -X POST /embed` with two texts returns two vectors of the documented dimension. ✅ Verified 2026-07-12 (PT/EN pair → 2×384).

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
