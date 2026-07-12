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

- [x] `internal/document`: `EmbeddingService` and `TextExtractor` ports defined at the consumer; migration `000003` adds `embedding vector(384)`
- [x] `platform/aiclient`: HTTP client implementing `EmbeddingService` against `/embed` (120s timeout for cold model, `context` propagated, errors wrapped with service detail, batches of 128 under the 256 contract cap)
- [x] Text extraction: PDF via the `pdftotext` binary (machine dependency, recorded in README) and plain text/markdown passthrough, behind the extractor port
- [x] Chunking: fixed-size with overlap, rune-safe (Portuguese accents never cut mid-UTF-8); pure function tested for empty/huge/no-space/multibyte inputs
- [x] Worker pool: dispatcher claims via `SKIP LOCKED` and feeds N workers over an unbuffered channel — backpressure keeps backlog in Postgres, not memory; shutdown requeues cancellation-interrupted documents instead of failing them
- [x] Status transitions with error persisted on failure; `POST /documents/{id}/retry` requeues failed docs (409 for non-failed)
- [x] Tests per stage + integration test against a dedicated `askdocs_test` database (`TEST_DATABASE_URL`), covering claim semantics, 384-dim persistence and idempotent re-save

**Done when:** uploading a real PDF ends with the document `ready` and its chunks + vectors visible in the `chunks` table. ✅ Verified 2026-07-12 (queued→ready in ~3s; the Phase-2 stub PDF correctly went `failed` with the pdftotext error persisted; SIGTERM drained cleanly).

---

## Phase 5 — Query flow (RAG) end to end

Goal: a question comes in through Go and an answer with citations comes out.

> **Decision (recorded):** Go owns retrieval — it embeds the question via
> `/embed`, searches pgvector through its `VectorStore` port, and sends the
> top-5 chunks to Python `POST /generate`. Python stays stateless (no DB).
> LLM: `claude-opus-4-8` via the official Anthropic SDK with structured output
> (`messages.parse` + Pydantic), env-configurable via `LLM_MODEL`.

- [x] `platform/postgres`: `VectorStore.Search` using pgvector cosine (`<=>`), ready documents only — exact scan for now (HNSW deferred until scale demands)
- [x] Python `POST /generate`: contract `{question, chunks: [{id, document_id, text}]} → {answer, citations: [{chunk_id, document_id}]}`; grounding system prompt (excerpts-only, question's language, graceful "not found"); refusal stop-reason handled; missing key → clear 502
- [x] `internal/query`: `Conversation`/`Message`/`Citation` entities; `Repository`/`EmbeddingService`/`VectorStore`/`LLMService` ports; Ask use-case (embed → retrieve → generate → persist user+assistant messages with display-ready citations); zero chunks short-circuits without an LLM call
- [x] `platform/aiclient`: `LLMService` against `/generate` (one adapter satisfies both embedding and generation ports)
- [x] `platform/httpapi`: `POST /queries` (creates/continues a conversation, returns answer + citations with filename and snippet), `GET /conversations/{id}` (history)
- [x] Domain tests with mocked ports (hallucinated-citation guard included); Python contract tests with a mocked generator

**Done when:** with one ingested document, `curl POST /queries` returns a correct answer whose citations point at real chunks of that document. ⏳ Everything except the live LLM call verified 2026-07-12 (full chain runs; without `ANTHROPIC_API_KEY` it returns a clear 502 and persists the conversation). Set the key in `.env` and rerun to close the gate.

---

## Phase 6 — Next.js frontend

Goal: the whole loop usable from a browser.

- [x] Scaffold: `create-next-app` (Next 16, App Router, `strict: true`, ESLint, no Tailwind — no unrequested deps). Dev runs on **port 3001** (3000 is taken on this machine); `/api/*` is proxied to the Go API via Next rewrites, so the browser stays same-origin and no CORS is needed
- [x] Typed API client ([frontend/lib/api.ts](frontend/lib/api.ts)) — the only place URLs and response shapes live; API `{error}` bodies become typed `ApiError`s the UI renders
- [x] Upload page: file picker → `POST /documents` → list with status badges; polls only while something is `queued`/`processing`; failed docs show the error and a retry button
- [x] Chat page: question input, local message history, loading/error states; conversation continues via `conversation_id`
- [x] **Citations UI (core requirement):** each answer lists its sources — filename, expandable to the snippet
- [x] Server components by default; the only client components are the documents panel and the chat

**Done when:** a person with no curl knowledge can upload a PDF, watch it become ready, ask a question, and see the answer with its sources. ⏳ Verified 2026-07-12 through the running stack: upload via the Next proxy → `ready` in seconds with live badge data; the ask path works end-to-end and currently surfaces the graceful "AI service" error banner — the final answer rendering awaits the Anthropic credentials (same blocker as the Phase 5 gate).

---

## Phase 7 — Authentication

Goal: documents and conversations belong to a user; the API is no longer anonymous.

- [x] Migration `000005`: `users` + `sessions` (token stored by SHA-256); `user_id NOT NULL` on documents and conversations — pre-auth dev rows wiped on purpose rather than allowing a nullable owner
- [x] `internal/auth`: register + login (bcrypt, dummy-hash on unknown email against user enumeration), opaque session token in an httpOnly SameSite=Lax cookie (decision recorded), 7-day TTL, lazy expiry cleanup, instant revocation on logout
- [x] `requireAuth` middleware — everything except `/healthz` and `/auth/*` requires a session; handlers read the user from the request context
- [x] Ownership in the domain layer: scoped `Get`/`List`/`GetConversation`, and `VectorStore.Search` filters by `user_id` — another user's chunks can never reach the LLM prompt; cross-user access is indistinguishable from 404
- [x] Frontend: `/login` (login/register toggle), session-aware user menu with logout, 401 → `/login` redirect in every client component
- [x] Tests: 401 sweep over every protected route; two-user isolation at handler, domain and SQL level (integration test proves Search never leaks)

**Done when:** two users each upload a document and neither can see or get answers from the other's content. ✅ Verified 2026-07-12 live: alice/bob registered, each sees only their own documents and conversations; unauthenticated → 401; logout revokes the cookie immediately; cookie flows through the Next proxy.

---

## Phase 8 — Hardening and polish

Goal: the MVP is demonstrable, documented, and honest about failures.

- [x] One scripted end-to-end smoke test (`scripts/smoke.sh`): starts whatever isn't running (compose, migrations, both services), registers a throwaway user, uploads a real PDF, waits for `ready`, asks, and asserts the citations reference the uploaded document; without credentials it asserts the clean 502 instead (`SMOKE_REQUIRE_LLM=1` makes the LLM leg mandatory)
- [x] Failure-path review — the smoke test immediately paid for itself: an **empty** `ANTHROPIC_API_KEY=` (the `.env` placeholder!) passed SDK construction and blew up as an opaque 500; now detected and reported as the friendly "no credentials" 502 (regression-tested). The AI service's sanitized `detail` now travels to the end user via a typed `query.AIUnavailableError` (defined in the consuming package, per CLAUDE.md) instead of dying in a generic message. LLM timeout: Python times out at 90s, before Go's 120s deadline, with a specific message. Malformed PDF (Phase 4) and oversized upload (new 413 test) covered
- [x] Rate limiting (per-IP token bucket, 10 req/s burst 30, `/healthz` exempt, 429 + Retry-After) + body caps reviewed (uploads 20 MiB, questions 8 KiB, auth 4 KiB)
- [x] CI pipeline (`.github/workflows/ci.yml`): Go (gofmt, vet, test + poppler-utils), Python (ruff check/format, pytest — ruff added as a dev dep with a small bug-focused config), frontend (lint, build)
- [x] README finalized: architecture diagram, from-scratch setup, one command per task, failure-behavior and API tables
- [x] SSE streaming: **skipped on purpose** — optional and not requested (CLAUDE.md: no unrequested features)

**Done when:** a fresh clone reaches a working demo by following the README only. ✅ Verified 2026-07-12: `./scripts/smoke.sh` passes from cold in ~6s (register → upload → ready → ask), exits 0 and leaves no stray processes. The LLM leg of the smoke (`SMOKE_REQUIRE_LLM=1`) is the same pending Phase 5 gate — runs green once Anthropic credentials exist.

---

## Explicitly out of scope for the MVP

Per CLAUDE.md, do not build unless explicitly requested: Redis, gRPC (HTTP is
fine), multi-file formats beyond PDF/text, document sharing, admin panels,
observability stacks, or any "for the future" abstraction.
