"""Grounded answer generation behind a single seam (like app/embeddings.py).

Model decision (MVP): claude-opus-4-8 via the official Anthropic SDK, with
structured output (messages.parse + Pydantic) so the answer and the cited
chunk ids come back validated — no JSON scraping. Swapping models is an env
var (LLM_MODEL); swapping providers means changing only this module.
"""

import os
from functools import lru_cache

import anthropic
from pydantic import BaseModel, Field

from .schemas import GenerateChunk

DEFAULT_MODEL = "claude-opus-4-8"

SYSTEM_PROMPT = """\
You answer questions about the user's own documents.

Rules:
- Base your answer ONLY on the provided excerpts. Never use outside knowledge.
- If the excerpts do not contain the answer, say you could not find it in the
  documents — do not guess.
- Answer in the same language as the question.
- In `citations`, list the id of every excerpt you actually used. If you could
  not answer from the excerpts, leave it empty.
- Be concise and direct."""


class GeneratedAnswer(BaseModel):
    """Structured output contract enforced on the model response."""

    answer: str = Field(description="The answer, grounded only in the excerpts, in the question's language.")
    citations: list[str] = Field(
        description="Ids of the excerpts that support the answer. Empty when the excerpts don't contain it."
    )


class GenerationError(Exception):
    """The LLM provider failed or refused; the caller maps this to HTTP."""


class Generator:
    def __init__(self, model: str):
        self.model = model
        # Lazy: anthropic.Anthropic() resolves credentials from a chain
        # (ANTHROPIC_API_KEY → ANTHROPIC_AUTH_TOKEN → `ant auth login`
        # profile), and raises when none exist. Defer that to request time so
        # /healthz and /embed keep working without credentials.
        self._client: anthropic.Anthropic | None = None

    def _client_or_error(self) -> anthropic.Anthropic:
        if self._client is None:
            try:
                self._client = anthropic.Anthropic()
            except Exception as exc:
                raise GenerationError(
                    "no Anthropic credentials found — set ANTHROPIC_API_KEY or run `ant auth login`"
                ) from exc
        return self._client

    def generate(self, question: str, chunks: list[GenerateChunk]) -> GeneratedAnswer:
        client = self._client_or_error()
        excerpts = "\n\n".join(
            f'<excerpt id="{chunk.id}">\n{chunk.text}\n</excerpt>' for chunk in chunks
        )
        try:
            response = client.messages.parse(
                model=self.model,
                max_tokens=16000,
                thinking={"type": "adaptive"},
                system=SYSTEM_PROMPT,
                messages=[
                    {
                        "role": "user",
                        "content": f"{excerpts}\n\nQuestion: {question}",
                    }
                ],
                output_format=GeneratedAnswer,
            )
        except anthropic.AuthenticationError as exc:
            raise GenerationError(
                "LLM provider rejected the credentials — set ANTHROPIC_API_KEY"
            ) from exc
        except anthropic.APIStatusError as exc:
            raise GenerationError(f"LLM provider error ({exc.status_code}): {exc.message}") from exc
        except anthropic.APIConnectionError as exc:
            raise GenerationError("could not reach the LLM provider") from exc

        if response.stop_reason == "refusal":
            return GeneratedAnswer(
                answer="I can't help with that question.", citations=[]
            )
        if response.parsed_output is None:
            raise GenerationError("LLM returned output that does not match the schema")
        return response.parsed_output


@lru_cache(maxsize=1)
def get_generator() -> Generator:
    return Generator(os.environ.get("LLM_MODEL", DEFAULT_MODEL))
