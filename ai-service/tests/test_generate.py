"""Contract tests for /generate. The real Anthropic client is never touched:
the generator is faked via dependency override — what matters is the boundary
shape the Go aiclient depends on, and the citation hallucination guard."""

from fastapi.testclient import TestClient

from app.generation import GeneratedAnswer, GenerationError, get_generator
from app.main import app


class FakeGenerator:
    def __init__(self, result: GeneratedAnswer | None = None, error: str | None = None):
        self.result = result
        self.error = error
        self.calls: list[tuple[str, int]] = []

    def generate(self, question, chunks):
        self.calls.append((question, len(chunks)))
        if self.error:
            raise GenerationError(self.error)
        return self.result


client = TestClient(app, raise_server_exceptions=False)

CHUNKS = [
    {"id": "c1", "document_id": "d1", "text": "O contrato prevê rescisão em 30 dias."},
    {"id": "c2", "document_id": "d2", "text": "O pagamento é mensal."},
]


def override(generator: FakeGenerator) -> None:
    app.dependency_overrides[get_generator] = lambda: generator


def teardown_function() -> None:
    app.dependency_overrides.pop(get_generator, None)


def test_generate_returns_answer_and_citations():
    override(FakeGenerator(GeneratedAnswer(answer="30 dias.", citations=["c1"])))

    resp = client.post("/generate", json={"question": "Qual o prazo?", "chunks": CHUNKS})

    assert resp.status_code == 200
    body = resp.json()
    assert body["answer"] == "30 dias."
    assert body["citations"] == [{"chunk_id": "c1", "document_id": "d1"}]


def test_generate_drops_hallucinated_and_duplicate_citations():
    override(FakeGenerator(GeneratedAnswer(answer="ok", citations=["c2", "c-fake", "c2"])))

    resp = client.post("/generate", json={"question": "q?", "chunks": CHUNKS})

    assert resp.status_code == 200
    assert resp.json()["citations"] == [{"chunk_id": "c2", "document_id": "d2"}]


def test_generate_empty_chunks_is_422():
    override(FakeGenerator(GeneratedAnswer(answer="x", citations=[])))

    resp = client.post("/generate", json={"question": "q?", "chunks": []})

    assert resp.status_code == 422


def test_generate_missing_question_is_422():
    override(FakeGenerator(GeneratedAnswer(answer="x", citations=[])))

    resp = client.post("/generate", json={"chunks": CHUNKS})

    assert resp.status_code == 422


def test_generate_provider_failure_is_502_with_detail():
    override(FakeGenerator(error="LLM provider rejected the credentials — set ANTHROPIC_API_KEY"))

    resp = client.post("/generate", json={"question": "q?", "chunks": CHUNKS})

    assert resp.status_code == 502
    assert "ANTHROPIC_API_KEY" in resp.json()["detail"]
