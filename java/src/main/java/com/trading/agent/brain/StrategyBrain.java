package com.trading.agent.brain;

import com.trading.config.AgentProperties;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;

/**
 * Fear & Greed Contrarian Strategy.
 *
 * Logic:
 *   RSI < rsiOversold AND news score > sentimentFloor AND no open position → LONG
 *   RSI > rsiOverbought AND has open position                               → FLAT
 *   Otherwise                                                                → HOLD
 *
 * RSI acts as a proxy for market fear/greed:
 *   - Oversold (RSI < 35) = market in fear, good time to enter
 *   - Overbought (RSI > 65) = market in greed, time to exit
 *
 * News sentiment filter prevents buying into structurally bearish news
 * (e.g., regulatory crackdowns, major exploits).
 */
@Component
public class StrategyBrain {

    private static final Logger log = LoggerFactory.getLogger(StrategyBrain.class);

    public enum Action { LONG, FLAT, HOLD }

    public record Signal(Action action, String asset, String reason) {}

    private final AgentProperties.Strategy cfg;

    public StrategyBrain(AgentProperties props) {
        this.cfg = props.strategy();
    }

    /**
     * Evaluate market conditions and return a trading signal.
     *
     * @param asset           trading pair, e.g. "ETH-USDC"
     * @param rsi             latest RSI(14) value
     * @param newsSentiment   news sentiment score, -1.0 to 1.0
     * @param hasOpenPosition true if the agent already has a position in this asset
     */
    public Signal evaluate(String asset, double rsi, double newsSentiment, boolean hasOpenPosition) {
        // LONG: fear signal confirmed by non-negative news
        if (rsi < cfg.rsiOversold() && newsSentiment > cfg.newsSentimentFloor() && !hasOpenPosition) {
            String reason = "Contrarian LONG: RSI=%.1f (< %d oversold), sentiment=%.2f (> %.1f floor)"
                    .formatted(rsi, cfg.rsiOversold(), newsSentiment, cfg.newsSentimentFloor());
            log.info("{}: LONG signal — {}", asset, reason);
            return new Signal(Action.LONG, asset, reason);
        }

        // FLAT: greed signal — exit existing position
        if (rsi > cfg.rsiOverbought() && hasOpenPosition) {
            String reason = "Contrarian FLAT: RSI=%.1f (> %d overbought)"
                    .formatted(rsi, cfg.rsiOverbought());
            log.info("{}: FLAT signal — {}", asset, reason);
            return new Signal(Action.FLAT, asset, reason);
        }

        // Explain why we hold (useful for debugging)
        String reason = buildHoldReason(asset, rsi, newsSentiment, hasOpenPosition);
        log.debug("{}: HOLD — {}", asset, reason);
        return new Signal(Action.HOLD, asset, reason);
    }

    private String buildHoldReason(String asset, double rsi, double sentiment, boolean hasPosition) {
        if (rsi < cfg.rsiOversold() && sentiment <= cfg.newsSentimentFloor()) {
            return "RSI oversold but news too bearish (score=%.2f)".formatted(sentiment);
        }
        if (rsi < cfg.rsiOversold() && hasPosition) {
            return "RSI oversold but already in position";
        }
        if (rsi > cfg.rsiOverbought() && !hasPosition) {
            return "RSI overbought but no position to exit";
        }
        return "RSI=%.1f in neutral zone [%d, %d]".formatted(rsi, cfg.rsiOversold(), cfg.rsiOverbought());
    }
}
