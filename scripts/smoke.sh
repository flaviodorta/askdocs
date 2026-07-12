#!/usr/bin/env bash
# End-to-end smoke test: infra up → upload a real PDF → wait until ready →
# ask a question → assert the answer cites the uploaded document.
#
# Usage:            ./scripts/smoke.sh
# Reuse a running stack: it detects a healthy API on $API_URL and skips startup.
# Force the LLM leg:     SMOKE_REQUIRE_LLM=1 ./scripts/smoke.sh
#   (without it, missing Anthropic credentials downgrade the ask step to a
#   check that the API degrades with a clear 502 — and the run still passes)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_URL="${API_URL:-http://localhost:8080}"
AI_URL="${AI_SERVICE_URL:-http://localhost:8000}"
FIXTURE="$ROOT/backend/internal/platform/extract/testdata/hello.pdf"
QUESTION="When can this agreement be terminated?"

TMP="$(mktemp -d)"
COOKIES="$TMP/cookies"
API_PID=""
AI_PID=""

cleanup() {
  status=$?
  if [ -n "$API_PID" ]; then kill "$API_PID" 2>/dev/null || true; wait "$API_PID" 2>/dev/null || true; fi
  if [ -n "$AI_PID" ]; then kill "$AI_PID" 2>/dev/null || true; wait "$AI_PID" 2>/dev/null || true; fi
  rm -rf "$TMP"
  exit "$status" # wait reports the killed services' 143 — keep the real result
}
trap cleanup EXIT

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m ok\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mFAIL\033[0m %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null || die "missing dependency: $1"; }
need curl; need python3

json_get() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get(sys.argv[1]) or "")' "$1"; }

# request METHOD PATH [curl args...] — body lands in $TMP/body, echoes status.
request() {
  local method="$1" path="$2"; shift 2
  curl -sS -o "$TMP/body" -w '%{http_code}' -X "$method" \
    -b "$COOKIES" -c "$COOKIES" "$API_URL$path" "$@"
}

wait_http() { # wait_http URL SECONDS LABEL
  local url="$1" deadline=$(( $(printf '%(%s)T' -1) + $2 )) label="$3"
  until curl -sf -o /dev/null "$url"; do
    [ "$(printf '%(%s)T' -1)" -ge "$deadline" ] && die "$label did not become healthy at $url"
    sleep 1
  done
}

# --- 1. Stack ---------------------------------------------------------------

# Make .env (Anthropic key, ports) visible to the services we may start.
if [ -f "$ROOT/.env" ]; then set -a; . "$ROOT/.env"; set +a; fi

if curl -sf -o /dev/null "$API_URL/healthz"; then
  say "reusing already-running stack at $API_URL"
else
  need docker; need go
  say "starting Postgres (docker compose)"
  (cd "$ROOT" && docker compose up -d --wait) >/dev/null
  say "applying migrations"
  (cd "$ROOT" && make migrate-up) >/dev/null

  if ! curl -sf -o /dev/null "$AI_URL/healthz"; then
    [ -x "$ROOT/ai-service/.venv/bin/uvicorn" ] \
      || die "ai-service venv missing — run: cd ai-service && python3 -m venv .venv && .venv/bin/pip install -e '.[dev]'"
    say "starting ai-service"
    # exec so $! is uvicorn itself, not a bash wrapper that swallows the kill.
    (cd "$ROOT/ai-service" && exec ./.venv/bin/uvicorn app.main:app --port "${AI_PORT:-8000}" >"$TMP/ai.log" 2>&1) &
    AI_PID=$!
    wait_http "$AI_URL/healthz" 60 "ai-service"
  fi

  say "building and starting Go API"
  (cd "$ROOT/backend" && go build -o "$TMP/api" ./cmd/api)
  (cd "$ROOT/backend" && exec "$TMP/api" >"$TMP/api.log" 2>&1) &
  API_PID=$!
  wait_http "$API_URL/healthz" 30 "Go API"
fi
ok "stack healthy"

# --- 2. Register a throwaway user --------------------------------------------

EMAIL="smoke-$$-$RANDOM@example.com"
say "registering $EMAIL"
status="$(request POST /auth/register -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"smoke-password\"}")"
[ "$status" = 201 ] || die "register: expected 201, got $status: $(cat "$TMP/body")"
ok "registered and logged in"

# --- 3. Upload the fixture PDF ------------------------------------------------

say "uploading hello.pdf"
status="$(request POST /documents -F "file=@$FIXTURE;type=application/pdf")"
[ "$status" = 202 ] || die "upload: expected 202, got $status: $(cat "$TMP/body")"
DOC_ID="$(json_get id < "$TMP/body")"
[ -n "$DOC_ID" ] || die "upload response has no id: $(cat "$TMP/body")"
ok "queued as $DOC_ID"

# --- 4. Wait for ingestion (first run may download the embedding model) ------

say "waiting for ingestion"
for _ in $(seq 1 150); do
  status="$(request GET "/documents/$DOC_ID")"
  [ "$status" = 200 ] || die "document status: expected 200, got $status"
  DOC_STATUS="$(json_get status < "$TMP/body")"
  case "$DOC_STATUS" in
    ready) break ;;
    failed) die "ingestion failed: $(json_get error < "$TMP/body")" ;;
    *) sleep 2 ;;
  esac
done
[ "$DOC_STATUS" = ready ] || die "document still '$DOC_STATUS' after 5 minutes"
ok "document ready"

# --- 5. Ask and assert the citation -------------------------------------------

say "asking: $QUESTION"
status="$(request POST /queries -H 'Content-Type: application/json' \
  -d "{\"question\":\"$QUESTION\"}")"

if [ "$status" = 200 ]; then
  ANSWER="$(json_get answer < "$TMP/body")"
  CITED="$(python3 -c '
import json, sys
d = json.load(sys.stdin)
cits = d.get("citations") or []
docs = {c.get("document_id") for c in cits}
print(len(cits), "hit" if sys.argv[1] in docs else "miss")
' "$DOC_ID" < "$TMP/body")"
  COUNT="${CITED% *}"; HIT="${CITED#* }"
  [ -n "$ANSWER" ] || die "empty answer"
  [ "$COUNT" -ge 1 ] || die "answer has no citations: $(cat "$TMP/body")"
  [ "$HIT" = hit ] || die "citations do not reference the uploaded document"
  ok "answer cites the uploaded document ($COUNT citation(s))"
  printf '\n    "%s"\n\n' "$ANSWER"
  say "SMOKE PASSED (full path, LLM included)"
elif [ "$status" = 502 ]; then
  ERR="$(json_get error < "$TMP/body")"
  [ -n "$ERR" ] || die "502 without a readable error body"
  if [ "${SMOKE_REQUIRE_LLM:-0}" = 1 ]; then
    die "LLM leg required but failed: $ERR"
  fi
  ok "API degraded cleanly without LLM credentials: \"$ERR\""
  say "SMOKE PASSED (retrieval path only — set ANTHROPIC_API_KEY to exercise the LLM leg)"
else
  die "ask: expected 200 (or 502 without credentials), got $status: $(cat "$TMP/body")"
fi
