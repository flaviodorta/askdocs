import logging
from typing import Annotated

from fastapi import Depends, FastAPI, Request
from fastapi.responses import JSONResponse

from .embeddings import Embedder, get_embedder
from .schemas import EmbedRequest, EmbedResponse

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
