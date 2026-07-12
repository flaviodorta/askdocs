"""Embedding generation behind a single seam.

Model decision (MVP): sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2
(384 dimensions), served locally through fastembed/ONNX — multilingual because
the user's documents are likely in Portuguese, and free of API keys and of a
PyTorch install. Swapping providers later means changing only this module (and
a pgvector migration if the dimension changes).
"""

import os
from functools import lru_cache

from fastembed import TextEmbedding

DEFAULT_MODEL = "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2"


class Embedder:
    def __init__(self, model_name: str):
        self.model_name = model_name
        self._model = TextEmbedding(model_name=model_name)
        # Probe once instead of trusting a hardcoded table: the dimension must
        # match whatever the loaded model actually produces.
        self.dim = len(next(iter(self._model.embed(["dimension probe"]))))

    def embed(self, texts: list[str]) -> list[list[float]]:
        return [vector.tolist() for vector in self._model.embed(texts)]


@lru_cache(maxsize=1)
def get_embedder() -> Embedder:
    return Embedder(os.environ.get("EMBEDDING_MODEL", DEFAULT_MODEL))
