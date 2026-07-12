import logging
from typing import Annotated

from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse

from .embeddings import Embedder, get_embedder
from .generation import GenerationError, Generator, get_generator
from .schemas import (
    EmbedRequest,
    EmbedResponse,
    GenerateCitation,
    GenerateRequest,
    GenerateResponse,
)

logger = logging.getLogger("askdocs-ai")

app = FastAPI(title="askdocs-ai", version="0.1.0")


@app.exception_handler(Exception)
async def unhandled_error(request: Request, exc: Exception) -> JSONResponse:
    logger.exception("unhandled error on %s %s", request.method, request.url.path)
    return JSONResponse(status_code=500, content={"detail": "internal error"})


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/embed")
def embed(
    req: EmbedRequest,
    embedder: Annotated[Embedder, Depends(get_embedder)],
) -> EmbedResponse:
    return EmbedResponse(
        embeddings=embedder.embed(req.texts),
        model=embedder.model_name,
        dim=embedder.dim,
    )


@app.post("/generate")
def generate(
    req: GenerateRequest,
    generator: Annotated[Generator, Depends(get_generator)],
) -> GenerateResponse:
    try:
        result = generator.generate(req.question, req.chunks)
    except GenerationError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    # Keep only citations that reference chunks we were actually given — the
    # model can hallucinate ids. Deduped, in request order.
    by_id = {chunk.id: chunk for chunk in req.chunks}
    seen: set[str] = set()
    citations = []
    for cited_id in result.citations:
        if cited_id in by_id and cited_id not in seen:
            seen.add(cited_id)
            citations.append(
                GenerateCitation(chunk_id=cited_id, document_id=by_id[cited_id].document_id)
            )
    return GenerateResponse(answer=result.answer, citations=citations)
