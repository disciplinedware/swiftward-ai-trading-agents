from common.cache import RedisCache
from common.exceptions import MCPError
from common.log import get_logger
from news_mcp.infra.base import NewsClient, NewsPost
from news_mcp.service.llm import AnalysisResult, NewsLLMScorer

logger = get_logger(__name__)

_CACHE_TTL = 60 * 10  # 10 minutes
_HEADLINES_KEY = "news:headlines:all"
_MACRO_KEY = "news:analysis:macro"


class NewsService:
    def __init__(
        self,
        client: NewsClient,
        scorer: NewsLLMScorer,
        cache: RedisCache,
        tracked_assets: list[str],
    ) -> None:
        self._client = client
        self._scorer = scorer
        self._cache = cache
        self._tracked_assets = tracked_assets

    async def get_headlines(self) -> list[NewsPost]:
        """Return all recent headlines as a flat list. Cached under a single key."""
        cached = await self._cache.get(_HEADLINES_KEY)
        if cached is not None:
            logger.debug("headlines cache hit", count=len(cached))
            return cached  # type: ignore[return-value]

        logger.debug("fetching headlines from CryptoPanic", assets=self._tracked_assets)
        posts = await self._client.get_posts(self._tracked_assets)
        logger.debug("CryptoPanic returned posts", count=len(posts))
        await self._cache.set(_HEADLINES_KEY, posts, _CACHE_TTL)
        return posts

    async def get_sentiment(self, assets: list[str]) -> dict[str, float]:
        """Return per-asset sentiment score -1.0 to +1.0. Cached per asset."""
        logger.debug("get_sentiment called", assets=assets)
        analysis = await self._get_analysis(assets)
        logger.debug("sentiment result", sentiment=analysis.sentiment)
        return analysis.sentiment

    async def get_macro_flag(self, assets: list[str]) -> dict:
        """Return global macro event flag {triggered, reason}. Cached at news:analysis:macro."""
        logger.debug("get_macro_flag called", assets=assets)
        cached_macro = await self._cache.get(_MACRO_KEY)
        if cached_macro is not None:
            logger.debug("macro flag cache hit", triggered=cached_macro.get("triggered"))  # type: ignore[union-attr]
            return cached_macro  # type: ignore[return-value]

        analysis = await self._get_analysis(assets)
        result = {"triggered": analysis.macro_flag, "reason": analysis.macro_reason}
        logger.debug("macro flag result", **result)
        return result

    async def _get_analysis(self, assets: list[str]) -> AnalysisResult:
        """Fetch per-asset analysis, calling LLM only for uncached assets."""
        cached_scores: dict[str, float] = {}
        uncached: list[str] = []

        for asset in assets:
            cached = await self._cache.get(_analysis_key(asset))
            if cached is not None:
                cached_scores[asset] = float(cached["sentiment"])  # type: ignore[index]
                logger.debug("analysis cache hit", asset=asset, sentiment=cached_scores[asset])
            else:
                uncached.append(asset)

        cached_macro = await self._cache.get(_MACRO_KEY)
        logger.debug(
            "analysis cache status",
            cached=list(cached_scores.keys()),
            uncached=uncached,
            macro_cached=cached_macro is not None,
        )

        if not uncached and cached_macro is not None:
            logger.debug("all analysis served from cache, skipping LLM call")
            return AnalysisResult(
                sentiment=cached_scores,
                macro_flag=bool(cached_macro["triggered"]),  # type: ignore[index]
                macro_reason=cached_macro.get("reason"),  # type: ignore[union-attr]
            )

        headlines = await self.get_headlines()

        llm_assets = uncached if uncached else assets
        logger.info("calling news LLM", assets=llm_assets)
        try:
            analysis = await self._scorer.analyze(llm_assets, headlines)
        except Exception as exc:
            logger.error("news LLM analysis failed", assets=llm_assets, error=str(exc))
            raise MCPError(f"news LLM analysis failed: {exc}") from exc

        logger.info(
            "news LLM analysis complete",
            sentiment=analysis.sentiment,
            macro_triggered=analysis.macro_flag,
            macro_reason=analysis.macro_reason,
        )

        for asset in llm_assets:
            score = analysis.sentiment.get(asset, 0.0)
            await self._cache.set(_analysis_key(asset), {"sentiment": score}, _CACHE_TTL)
            cached_scores[asset] = score

        if cached_macro is None:
            macro_entry = {"triggered": analysis.macro_flag, "reason": analysis.macro_reason}
            await self._cache.set(_MACRO_KEY, macro_entry, _CACHE_TTL)

        return AnalysisResult(
            sentiment=cached_scores,
            macro_flag=(
                analysis.macro_flag if cached_macro is None else bool(cached_macro["triggered"])  # type: ignore[index]
            ),
            macro_reason=(
                analysis.macro_reason if cached_macro is None else cached_macro.get("reason")  # type: ignore[union-attr]
            ),
        )


def _analysis_key(asset: str) -> str:
    return f"news:analysis:{asset}"
