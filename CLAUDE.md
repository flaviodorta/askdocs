# CLAUDE.md

Instructions for AI agents (Claude Code) working in this repository. Read this
before writing or changing code. If a decision contradicts this file, stop and
ask instead of assuming.

---

## What this project is

`askdocs` is a question-and-answer assistant over the user's own documents
(RAG pattern — Retrieval-Augmented Generation). The user uploads PDFs/text files
and asks questions in natural language; the system answers **always citing the
source** (which excerpt of which document supported the answer).

The MVP goal is deliberately small in features and big in architecture:
upload a document → ask → get an answer with a citation. Do not add new
features unless they are explicitly requested. Quality here is measured by the
clarity of the architecture, not by the number of features.

---

## Architecture: three polyglot services

The system has three services, and Go is the central orchestrator. Each language
does what it does best — do not mix responsibilities.

1. **Next.js frontend (TypeScript)** — document upload and chat interface with
   source display. UI and calls to the Go API only. No business or AI logic
   lives here.

2. **Go backend (Hexagonal / Ports & Adapters architecture)** — the core. It owns:
   the HTTP API, authentication, and the asynchronous orchestration of the
   ingestion pipeline. Go does **not** generate embeddings or call the LLM
   directly — it talks to the Python service through a port.

3. **Python AI service (FastAPI)** — owns the ML. Generates embeddings, runs
   retrieval, and builds the answer via LLM. Exposed to Go over HTTP/gRPC.
   It is an *adapter*, not the center of the system.

Data: **Postgres + pgvector** stores documents, chunks, and vectors. (Optionally
Redis for the ingestion queue — start with a Postgres-backed queue and only add
Redis if there is a real need.)

### Boundaries that must NOT be crossed

- Embeddings and LLM calls live **exclusively in Python**. Go only knows the
  `EmbeddingService` and `LLMService` interfaces — never *how* they work.
- Business rules live **in the Go domain**, not in the frontend nor in the
  Python service.
- The frontend never talks directly to Python or to the database. All traffic
  goes through Go.

---

## Go backend structure

Organize **by domain/feature**, not by technical layer. Do not create global
`controllers/`, `services/`, `repositories/` folders. The approximate layout:

```
/backend
  /cmd/api            # entrypoint (main.go), dependency wiring
  /internal
    /document         # domain: Document, Chunk; ports and ingestion logic
    /query            # domain: Query, Conversation, Message
    /auth             # authentication domain
    /platform         # concrete adapters: postgres, aiclient (Python), http
      /postgres       # implements VectorStore and the repositories
      /aiclient       # implements EmbeddingService and LLMService (calls Python)
      /httpapi        # HTTP handlers, the primary adapter
  /migrations         # Postgres/pgvector SQL migrations
```

Each domain package contains its own entities, ports (interfaces), and logic.
Concrete adapters live in `platform/` and implement those interfaces.

---

## Go conventions (read carefully)

This is where agents make the most mistakes. Go has a culture of simplicity
that clashes with Java/C# habits. **Do not import the .NET playbook here.**

- **Interfaces are defined in the package that CONSUMES them, not the one that
  implements them.** E.g.: the `document` package defines `EmbeddingService`;
  the `aiclient` package implements it. This gives you dependency inversion for
  free — no DI framework, no magic container, no annotations.
- **Composition, not inheritance.** Go has no inheritance — do not try to
  simulate it with reflexive struct embedding. Prefer small interfaces.
- **Only add an abstraction when it pays for its own cost.** Do not create an
  interface for every struct. No factory of factories. Do not stack five layers
  of indirection. If there is a single implementation and no test needs a mock,
  it probably does not need an interface yet.
- **Errors are explicit.** Return `error`, wrap with `fmt.Errorf("...: %w", err)`
  to preserve the chain. No `panic` for control flow.
- **`context.Context` is the first parameter** of any function that does IO,
  network calls, or cancellable work. Propagate it all the way down.
- **Dependency injection is manual**, done in `cmd/api/main.go`: instantiate the
  concrete adapters and pass them to the constructors of the domain services.

### Ingestion pipeline (concurrency)

Uploads do not block the request. The handler enqueues the document and responds
immediately. A worker pool (goroutines) consumes the queue, extracts text, does
the chunking, calls Python for embeddings, and persists the vectors.

- Use channels and a fixed number of workers; respect `context` for cancellation
  and graceful shutdown.
- Think about backpressure — the queue has a limit; the producer waits when it
  is full.
- Each stage of the pipeline must be testable in isolation.

---

## Python service conventions

- FastAPI, typed with Pydantic on the input/output contracts.
- Single responsibility: embeddings, retrieval, and LLM generation. No auth,
  no document business rules.
- Lean, explicit endpoints (e.g.: `/embed`, `/generate`). The contract with Go
  is the boundary — changes to it must be reflected in the `aiclient` adapter.
- If LLM response streaming is implemented, expose it via SSE and let Go relay
  the stream to the frontend.

---

## Next.js frontend conventions

- App Router, strict TypeScript (`strict: true`). No `any` without justification.
- Server components by default; client components only where there is interaction.
- All communication goes to the Go API. Do not call Python or the database from
  here.
- Displaying sources/citations is a core part of the UX — treat it as a
  requirement, not decoration.

---

## Commands

Adjust to the repository's real setup; the intent is one command per task.

```bash
# Go backend
cd backend
go run ./cmd/api            # run the API locally
go test ./...              # run all tests
go vet ./... && gofmt -l . # lint and formatting check

# Python service
cd ai-service
uvicorn app.main:app --reload   # run the AI service
pytest                          # tests

# Frontend
cd frontend
npm run dev                # development environment
npm run build              # production build
npm run lint               # lint

# Local infra
docker compose up -d       # Postgres+pgvector and other dependencies
```

---

## When working in this repository

- Before writing code, confirm which of the three services the change belongs
  to, respecting the boundaries above. If a feature seems to require crossing
  boundaries, stop and explain the trade-off before coding.
- Write or update tests together with the change. In Go, test domain logic by
  mocking the ports.
- Prefer the simplest solution that solves the problem. If you catch yourself
  creating an abstraction "for the future", remove it until the future arrives.
- Small commits with messages that tell the decision, not just the "what".
- Keep the README and this CLAUDE.md updated when an architecture decision
  changes.

---

## Anti-patterns (do not do)

- Generating embeddings or calling the LLM inside Go code.
- Putting business rules in the frontend or in the Python service.
- Creating an interface for everything, unnecessary layers of indirection, or a
  DI framework in Go.
- Blocking the upload request by processing the document synchronously.
- Having the frontend access Python or the database directly.
- Adding a dependency, service (e.g.: Redis), or feature nobody asked for.
