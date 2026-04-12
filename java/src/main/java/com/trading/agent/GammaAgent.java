package com.trading.agent;

import com.trading.agent.brain.StrategyBrain;
import com.trading.agent.brain.StrategyBrain.Action;
import com.trading.agent.brain.StrategyBrain.Signal;
import com.trading.config.AgentProperties;
import com.trading.mcp.McpClient;
import com.trading.mcp.McpException;
import com.trading.mcp.dto.CandleResponse;
import com.trading.mcp.dto.NewsSentimentResponse;
import com.trading.mcp.dto.TradeResponse;
import jakarta.annotation.PostConstruct;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

import java.time.Duration;
import java.time.Instant;
import java.util.List;
import java.util.Map;
import java.util.OptionalDouble;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Gamma — Fear & Greed Contrarian Trading Agent.
 *
 * Wakes every 15 minutes (configurable via TICK_INTERVAL_MS).
 * For each tracked asset:
 *   1. Fetches RSI(14) via Market Data MCP
 *   2. Fetches news sentiment via News MCP (falls back to neutral on error)
 *   3. StrategyBrain decides: LONG / FLAT / HOLD
 *   4. Executes trade via Trading MCP (subject to policy engine)
 *
 * In-memory cooldown and position tracking reset on restart — intentional for
 * a hackathon demo where we want fresh decisions after redeploys.
 */
@Component
public class GammaAgent {

    private static final Logger log = LoggerFactory.getLogger(GammaAgent.class);

    private final McpClient mcp;
    private final StrategyBrain brain;
    private final AgentProperties props;

    // Tracks which assets we believe we hold (reset on restart)
    private final Set<String> openPositions = ConcurrentHashMap.newKeySet();

    // Maps asset → last trade timestamp for cooldown enforcement
    private final Map<String, Instant> lastTradeAt = new ConcurrentHashMap<>();

    public GammaAgent(McpClient mcp, StrategyBrain brain, AgentProperties props) {
        this.mcp = mcp;
        this.brain = brain;
        this.props = props;
    }

    @PostConstruct
    void logStartup() {
        log.info("Gamma agent started: id={}, assets={}, interval={}s, rsi=[{},{}]",
                props.id(),
                props.strategy().assets(),
                props.tickIntervalMs() / 1000,
                props.strategy().rsiOversold(),
                props.strategy().rsiOverbought());
    }

    @Scheduled(fixedDelayString = "${agent.tick-interval-ms:900000}")
    public void tick() {
        log.info("=== Gamma tick ===");
        for (String asset : props.strategy().assets()) {
            try {
                processAsset(asset);
            } catch (Exception e) {
                log.error("{}: unhandled error — {}", asset, e.getMessage());
            }
        }
    }

    // -------------------------------------------------------------------------

    private void processAsset(String asset) {
        if (isOnCooldown(asset)) {
            return;
        }

        // 1. RSI from market MCP
        OptionalDouble rsiOpt = fetchRsi(asset);
        if (rsiOpt.isEmpty()) {
            log.warn("{}: RSI unavailable (warm-up period), skipping", asset);
            return;
        }
        double rsi = rsiOpt.getAsDouble();

        // 2. News sentiment — fail-open: use neutral (0.0) if MCP unavailable
        double sentiment = fetchSentiment(asset);

        // 3. Brain decision
        boolean hasPosition = openPositions.contains(asset);
        Signal signal = brain.evaluate(asset, rsi, sentiment, hasPosition);

        log.info("{}: RSI={:.1f} sentiment={:.2f} position={} → {}",
                asset, rsi, sentiment, hasPosition, signal.action());

        // 4. Execute
        switch (signal.action()) {
            case LONG -> executeBuy(asset);
            case FLAT -> executeSell(asset);
            case HOLD -> { /* nothing to do */ }
        }
    }

    private OptionalDouble fetchRsi(String asset) {
        try {
            CandleResponse candles = mcp.call(
                    props.mcp().marketUrl(),
                    "market/get_candles",
                    Map.of(
                            "market", asset,
                            "interval", "1h",
                            "limit", 50,
                            "indicators", List.of("rsi_14")
                    ),
                    CandleResponse.class
            );
            return candles.latestRsi();
        } catch (McpException e) {
            log.error("{}: failed to fetch candles — {}", asset, e.getMessage());
            return OptionalDouble.empty();
        }
    }

    private double fetchSentiment(String asset) {
        String baseAsset = asset.split("-")[0]; // "ETH" from "ETH-USDC"
        try {
            NewsSentimentResponse news = mcp.call(
                    props.mcp().newsUrl(),
                    "news/get_sentiment",
                    Map.of(
                            "query", baseAsset,
                            "markets", List.of(baseAsset),
                            "period", "4h"
                    ),
                    NewsSentimentResponse.class
            );
            log.debug("{}: news sentiment={} score={}", asset, news.sentiment(), news.score());
            return news.score();
        } catch (Exception e) {
            log.warn("{}: news MCP unavailable ({}), using neutral sentiment 0.0", asset, e.getMessage());
            return 0.0;
        }
    }

    private void executeBuy(String asset) {
        try {
            TradeResponse result = mcp.call(
                    props.mcp().tradingUrl(),
                    "trade/submit_order",
                    Map.of("pair", asset, "side", "buy", "value", props.strategy().tradeValueUsd()),
                    TradeResponse.class
            );
            if (result.isFilled()) {
                log.info("BUY filled: {} qty={} @ {} (hash={})",
                        asset, result.fill().qty(), result.fill().price(), result.decisionHash());
                openPositions.add(asset);
                lastTradeAt.put(asset, Instant.now());
            } else {
                log.warn("BUY rejected for {}: [{}] {}",
                        asset, result.reject().source(), result.reject().reason());
            }
        } catch (McpException e) {
            log.error("BUY failed for {}: {}", asset, e.getMessage());
        }
    }

    private void executeSell(String asset) {
        try {
            TradeResponse result = mcp.call(
                    props.mcp().tradingUrl(),
                    "trade/submit_order",
                    Map.of("pair", asset, "side", "sell", "value", props.strategy().tradeValueUsd()),
                    TradeResponse.class
            );
            if (result.isFilled()) {
                log.info("SELL filled: {} qty={} @ {} (hash={})",
                        asset, result.fill().qty(), result.fill().price(), result.decisionHash());
                openPositions.remove(asset);
                lastTradeAt.put(asset, Instant.now());
            } else {
                log.warn("SELL rejected for {}: [{}] {}",
                        asset, result.reject().source(), result.reject().reason());
            }
        } catch (McpException e) {
            log.error("SELL failed for {}: {}", asset, e.getMessage());
        }
    }

    private boolean isOnCooldown(String asset) {
        Instant last = lastTradeAt.get(asset);
        if (last == null) {
            return false;
        }
        long elapsedMinutes = Duration.between(last, Instant.now()).toMinutes();
        if (elapsedMinutes < props.strategy().cooldownMinutes()) {
            log.debug("{}: cooldown active ({}/{} min)", asset, elapsedMinutes, props.strategy().cooldownMinutes());
            return true;
        }
        return false;
    }
}
