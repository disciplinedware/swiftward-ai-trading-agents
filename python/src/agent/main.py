import asyncio
import sys

from agent.brain.factory import make_brain
from agent.infra.fear_greed_mcp import FearGreedMCPClient
from agent.infra.go_trading_mcp import GoTradingMCPClient
from agent.infra.news_mcp import NewsMCPClient
from agent.infra.onchain_mcp import OnchainMCPClient
from agent.infra.price_feed_mcp import PriceFeedMCPClient
from agent.infra.trading_client import TradingClient
from agent.infra.trading_mcp import TradingMCPClient
from agent.trigger.clock import ClockLoop
from agent.trigger.cooldown import CooldownGate
from agent.trigger.exit_watchdog import ExitWatchdog
from agent.trigger.price_spike import PriceSpikeLoop
from agent.trigger.tier2 import Tier2Loop
from common.config import get_config
from common.log import get_logger, setup_logging


async def main() -> None:
    config = get_config()
    setup_logging(config)
    logger = get_logger(__name__)

    logger.info("agent starting")

    # Build MCP clients
    trading: TradingClient
    if config.trading.backend == "go":
        if not config.mcp_servers.go_trading_url or not config.trading.agent_id:
            logger.error(
                "trading.backend=go requires mcp_servers.go_trading_url and trading.agent_id"
            )
            sys.exit(1)
        trading = GoTradingMCPClient(config.mcp_servers.go_trading_url, config.trading.agent_id)
        logger.info(
            "using Go Trading MCP",
            url=config.mcp_servers.go_trading_url,
            agent_id=config.trading.agent_id,
        )
    else:
        trading = TradingMCPClient(config.mcp_servers.trading_url)
    price_feed = PriceFeedMCPClient(config.mcp_servers.price_feed_url)
    fear_greed = FearGreedMCPClient(config.mcp_servers.fear_greed_url)
    onchain = OnchainMCPClient(config.mcp_servers.onchain_data_url)
    news = NewsMCPClient(config.mcp_servers.news_url)

    # Parallel health checks
    results = await asyncio.gather(
        trading.health_check(),
        price_feed.health_check(),
        fear_greed.health_check(),
        onchain.health_check(),
        news.health_check(),
    )
    names = ["trading", "price_feed", "fear_greed", "onchain", "news"]
    failed = [name for name, ok in zip(names, results) if not ok]
    if failed:
        logger.error("MCP health checks failed", servers=failed)
        sys.exit(1)

    logger.info("MCP health checks passed")

    # Load initial portfolio
    portfolio = await trading.get_portfolio()
    logger.info(
        "portfolio loaded",
        open_positions=portfolio.open_position_count,
        balance=str(portfolio.stablecoin_balance),
    )

    # Wire brain and cooldown gate
    brain = make_brain(config)
    gate = CooldownGate(config.trigger, config.trading, trading)

    # Build exit watchdog (stop-loss / take-profit).
    # Disabled when backend=go: the Go trading server manages SL/TP server-side
    # (auto-created from submit_order params, evaluated by runPositionAlertPoller).
    exit_watchdog: ExitWatchdog | None = None
    if config.trading.backend != "go":
        exit_watchdog = ExitWatchdog(
            trading=trading,
            price_feed=price_feed,
            gate=gate,
        )

    # Build clock loop
    clock = ClockLoop(
        trading=trading,
        price_feed=price_feed,
        fear_greed=fear_greed,
        onchain=onchain,
        news=news,
        brain=brain,
        gate=gate,
        config=config,
    )

    # Build price spike loop (Task 19)
    price_spike = PriceSpikeLoop(
        price_feed=price_feed,
        gate=gate,
        clock=clock,
        config=config,
    )

    # Build tier2 loop (Task 20)
    tier2 = Tier2Loop(
        fear_greed=fear_greed,
        news=news,
        clock=clock,
        config=config,
    )

    logger.info("agent ready, starting loops")
    coros = [
        clock.run(),
        price_spike.run(),    # Task 19: price spike loop
        tier2.run(),          # Task 20: tier2 loop
    ]
    if exit_watchdog is not None:
        coros.append(exit_watchdog.run())  # Task 18: stop-loss / take-profit watchdog
    await asyncio.gather(*coros)


if __name__ == "__main__":
    asyncio.run(main())
