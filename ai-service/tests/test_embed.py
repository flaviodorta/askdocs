"""Contract tests for the /embed boundary. The real model is never loaded:
what matters here is the shape the Go aiclient adapter depends on."""

from fastapi.testclient import TestClient

from app.embeddings import get_embedder
from app.main import app


class FakeEmbedder:
    model_name = "fake-model"
    dim = 3

    def embed(self, texts: list[str]) -> list[list[float]]:
        return [[0.1, 0.2, 0.3] for _ in texts]


app.dependency_overrides[get_embedder] = lambda: FakeEmbedder()
client = TestClient(app, raise_server_exceptions=False)


def test_healthz():
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


def test_embed_returns_one_vector_per_text_in_order():
    resp = client.post("/embed", json={"texts": ["primeiro", "second"]})
    assert resp.status_code == 200
    body = resp.json()
    assert len(body["embeddings"]) == 2
    assert all(len(vec) == body["dim"] for vec in body["embeddings"])


def test_embed_reports_model_and_dim():
    resp = client.post("/embed", json={"texts": ["oi"]})
    body = resp.json()
    assert body["model"] == "fake-model"
    assert body["dim"] == 3


def test_embed_empty_list_is_422():
    resp = client.post("/embed", json={"texts": []})
    assert resp.status_code == 422


def test_embed_missing_field_is_422():
    resp = client.post("/embed", json={})
    assert resp.status_code == 422
