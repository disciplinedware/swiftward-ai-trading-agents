import asyncio
import json
from dataclasses import dataclass

from openai import AsyncOpenAI

from common.config import NewsLLMConfig
from common.log import get_logger
from news_mcp.infra.base import NewsPost

logger = get_logger(__name__)

_SYSTEM_PROMPT = (
    "You are a crypto market sentiment analyzer. Respond only in JSON with no markdown.\n"
    'Schema: {"sentiment": {"BTC": 0.0, ...}, "macro": {"triggered": false, "reason": null}}\n'
    "Sentiment: -1.0 (very bearish) to +1.0 (very bullish)."
    " Use 0.0 for assets with no relevant headlines.\n"
    "Macro flag: true for Fed/central bank policy changes, ETF approval/rejection, "
    "major exchange collapse or hack, or government regulatory action targeting crypto broadly."
)


@dataclass
class AnalysisResult:
    sentiment: dict[str, float]
    macro_flag: bool
    macro_reason: str | None


_LLM_TIMEOUT_SECONDS = 30


class NewsLLMScorer:
    def __init__(self, cfg: NewsLLMConfig) -> None:
        self._client = AsyncOpenAI(base_url=cfg.base_url, api_key=cfg.api_key)
        self._model = cfg.model
        self._max_tokens = cfg.max_tokens

    async def analyze(
        self, assets: list[str], headlines: list[NewsPost]
    ) -> AnalysisResult:
        user_msg = _build_prompt(assets, headlines)
        logger.debug("news LLM request", model=self._model, assets=assets, prompt=user_msg)
        try:
            resp = await asyncio.wait_for(
                self._client.chat.completions.create(
                    model=self._model,
                    messages=[
                        {"role": "system", "content": _SYSTEM_PROMPT},
                        {"role": "user", "content": user_msg},
                    ],
                    max_tokens=self._max_tokens,
                ),
                timeout=_LLM_TIMEOUT_SECONDS,
            )
        except asyncio.TimeoutError as exc:
            logger.error(
                "news LLM timed out",
                model=self._model,
                timeout_seconds=_LLM_TIMEOUT_SECONDS,
            )
            raise ValueError(
                f"news LLM timed out after {_LLM_TIMEOUT_SECONDS}s"
            ) from exc

        raw = resp.choices[0].message.content
        if not raw:
            logger.warning("news LLM returned empty response", model=self._model)

        content = raw or "{}"

        logger.debug("news LLM raw response", model=self._model, content=content)

        result = _parse_response(content, assets)

        logger.info(
            "news LLM parsed",
            model=self._model,
            sentiment=result.sentiment,
            macro_triggered=result.macro_flag,
            macro_reason=result.macro_reason,
        )

        return result


def _build_prompt(assets: list[str], headlines: list[NewsPost]) -> str:
    lines = [
        f"Assets to score: {', '.join(assets)}",
        "",
        "Recent news (assign relevance per asset):",
    ]
    if not headlines:
        lines.append("  (no recent news)")
    else:
        for h in headlines[:50]:
            lines.append(f'  - "{h["title"]}" ({h["published_at"]})')
            if h.get("description"):
                lines.append(f'    {h["description"]}')
    return "\n".join(lines)


def _strip_markdown_fences(content: str) -> str:
    """Strip ```json ... ``` or ``` ... ``` wrappers that some models add."""
    stripped = content.strip()
    if stripped.startswith("```"):
        first_newline = stripped.find("\n")
        if first_newline != -1:
            stripped = stripped[first_newline + 1:]
        if stripped.rstrip().endswith("```"):
            stripped = stripped.rstrip()[:-3].rstrip()
    return stripped


def _parse_response(content: str, assets: list[str]) -> AnalysisResult:
    cleaned = _strip_markdown_fences(content)
    if cleaned != content.strip():
        logger.debug("stripped markdown fences from LLM response")
    try:
        data = json.loads(cleaned)
    except json.JSONDecodeError as exc:
        logger.error("news LLM returned invalid JSON", content=content[:500])
        raise ValueError(f"news LLM returned invalid JSON: {content[:200]!r}") from exc

    raw_sentiment = data.get("sentiment", {})
    sentiment: dict[str, float] = {}
    for asset in assets:
        try:
            score = float(raw_sentiment.get(asset, 0.0))
            sentiment[asset] = max(-1.0, min(1.0, score))
        except (TypeError, ValueError):
            logger.warning(
                "invalid sentiment value for asset, defaulting to 0.0",
                asset=asset,
                raw_value=raw_sentiment.get(asset),
            )
            sentiment[asset] = 0.0

    macro = data.get("macro", {})
    triggered = bool(macro.get("triggered", False))
    reason = macro.get("reason") or None
    return AnalysisResult(sentiment=sentiment, macro_flag=triggered, macro_reason=reason)
