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

# The Go client gives up on /generate after 120s. Time the LLM call out before
# that so the caller gets a specific "LLM timed out" 502 instead of a dropped
# connection. (The SDK default is 10 minutes.)
LLM_TIMEOUT_SECONDS = 90.0

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

    answer: str = Field(
        description="The answer, grounded only in the excerpts, in the question's language."
    )
    citations: list[str] = Field(
        description="Ids of the excerpts that support the answer. Empty when the excerpts don't contain it."
    )


class GenerationError(Exception):
    """The LLM provider failed or refused; the caller maps this to HTTP."""


_NO_CREDENTIALS_MSG = (
    "no Anthropic credentials found — set ANTHROPIC_API_KEY or run `ant auth login`"
)


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
                client = anthropic.Anthropic(timeout=LLM_TIMEOUT_SECONDS)
            except Exception as exc:
                raise GenerationError(_NO_CREDENTIALS_MSG) from exc
            # An empty ANTHROPIC_API_KEY (e.g. the placeholder line in .env)
            # passes construction but blows up as a bare TypeError at request
            # time — treat "empty" the same as "absent", with the same message.
            if not (client.api_key or client.auth_token or client.credentials):
                raise GenerationError(_NO_CREDENTIALS_MSG)
            self._client = client
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
        except anthropic.APITimeoutError as exc:
            raise GenerationError(f"LLM did not answer within {LLM_TIMEOUT_SECONDS:.0f}s") from exc
        except anthropic.APIConnectionError as exc:
            raise GenerationError("could not reach the LLM provider") from exc

        if response.stop_reason == "refusal":
            return GeneratedAnswer(answer="I can't help with that question.", citations=[])
        if response.parsed_output is None:
            raise GenerationError("LLM returned output that does not match the schema")
        return response.parsed_output


@lru_cache(maxsize=1)
def get_generator() -> Generator:
    return Generator(os.environ.get("LLM_MODEL", DEFAULT_MODEL))
