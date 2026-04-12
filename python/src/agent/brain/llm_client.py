"""Thin async LLM client for brain stages.

Wraps openai.AsyncOpenAI with:
- XML+JSON response parsing (4-level fallback)
- Chinese/fullwidth character encoding normalization
- Exponential-backoff retry (configurable attempts)
"""
import asyncio
import json
import re

from openai import AsyncOpenAI

from common.exceptions import MCPError
from common.log import get_logger

logger = get_logger(__name__)

# Encoding normalization table: fullwidth / CJK punctuation → ASCII
_ENCODING_MAP = str.maketrans({
    "\u201c": '"',  # left double quotation mark "
    "\u201d": '"',  # right double quotation mark "
    "\uff3b": "[",  # fullwidth left square bracket ［
    "\uff3d": "]",  # fullwidth right square bracket ］
    "\uff5b": "{",  # fullwidth left curly bracket ｛
    "\uff5d": "}",  # fullwidth right curly bracket ｝
    "\uff1a": ":",  # fullwidth colon ：
    "\uff0c": ",",  # fullwidth comma ，
})


def _normalize_encoding(text: str) -> str:
    return text.translate(_ENCODING_MAP)


def _extract_reasoning(text: str) -> str:
    m = re.search(r"<reasoning>(.*?)</reasoning>", text, re.DOTALL)
    return m.group(1).strip() if m else ""


def _parse_decision(text: str) -> dict:
    """4-level fallback JSON extraction from LLM response text."""
    normalized = _normalize_encoding(text)

    # Level 1: content inside <decision> tag (strip markdown code fence)
    m = re.search(r"<decision>(.*?)</decision>", normalized, re.DOTALL)
    if m:
        candidate = m.group(1).strip()
        # Strip ```json ... ``` fence if present
        candidate = re.sub(r"^```[a-z]*\s*", "", candidate)
        candidate = re.sub(r"\s*```$", "", candidate)
        try:
            return json.loads(candidate)
        except json.JSONDecodeError:
            pass

    _decoder = json.JSONDecoder()

    # Level 2: from first '[' (array responses)
    idx = normalized.find("[")
    if idx != -1:
        try:
            obj, _ = _decoder.raw_decode(normalized, idx)
            return obj
        except json.JSONDecodeError:
            pass

    # Level 3: from first '{'
    idx = normalized.find("{")
    if idx != -1:
        try:
            obj, _ = _decoder.raw_decode(normalized, idx)
            return obj
        except json.JSONDecodeError:
            pass

    # Level 4: full response
    try:
        return json.loads(normalized)
    except json.JSONDecodeError:
        pass

    raise MCPError(f"LLM response contained no parseable JSON. Raw: {text[:200]!r}")


class LLMClient:
    """Async OpenAI-compatible LLM client for brain stages."""

    def __init__(
        self, base_url: str, model: str, api_key: str, max_tokens: int, retries: int
    ) -> None:
        _client = AsyncOpenAI(base_url=base_url, api_key=api_key)
        # Store completions directly to avoid Pylance misreading AsyncChat.__call__
        # as a MethodType (runtime: _client.chat is AsyncChat with .completions attr)
        self._completions = _client.chat.completions  # type: ignore[attr-defined]
        self._model = model
        self._max_tokens = max_tokens
        self._retries = retries

    async def call(self, system: str, user: str) -> tuple[str, dict]:
        """Call the LLM and return (reasoning: str, decision: dict).

        Retries up to self._retries times with 2*attempt second backoff.
        Raises MCPError if all retries fail or response is unparseable.
        """
        last_exc: Exception | None = None
        for attempt in range(self._retries):
            if attempt > 0:
                await asyncio.sleep(2 * attempt)
            try:
                response = await self._completions.create(
                    model=self._model,
                    max_tokens=self._max_tokens,
                    messages=[
                        {"role": "system", "content": system},
                        {"role": "user", "content": user},
                    ],
                )
                raw = response.choices[0].message.content or ""
                reasoning = _extract_reasoning(raw)
                decision = _parse_decision(raw)
                return reasoning, decision
            except MCPError:
                raise  # JSON parse failures are not retryable
            except Exception as exc:
                last_exc = exc
                logger.warning(
                    "LLM call failed, retrying",
                    attempt=attempt,
                    retries=self._retries,
                    error=str(exc),
                )

        raise last_exc  # type: ignore[misc]
