"""Pydantic contracts. These are the boundary with the Go backend: any change
here must be reflected in the aiclient adapter on the Go side."""

from pydantic import BaseModel, Field


class EmbedRequest(BaseModel):
    texts: list[str] = Field(
        min_length=1,
        max_length=256,
        description="Texts to embed; the response preserves this order.",
    )


class EmbedResponse(BaseModel):
    embeddings: list[list[float]]
    model: str
    dim: int


class GenerateChunk(BaseModel):
    id: str
    document_id: str
    text: str


class GenerateRequest(BaseModel):
    question: str = Field(min_length=1, max_length=8_192)
    chunks: list[GenerateChunk] = Field(
        min_length=1,
        max_length=50,
        description="Chunks retrieved by the Go backend; the answer must be grounded in these.",
    )


class GenerateCitation(BaseModel):
    chunk_id: str
    document_id: str


class GenerateResponse(BaseModel):
    answer: str
    citations: list[GenerateCitation]
