package com.trading.agent.brain;

import com.trading.config.AgentProperties;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.Arguments;
import org.junit.jupiter.params.provider.MethodSource;

import java.util.List;
import java.util.stream.Stream;

import static com.trading.agent.brain.StrategyBrain.Action.*;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.params.provider.Arguments.arguments;

class StrategyBrainTest {

    private static final StrategyBrain BRAIN = new StrategyBrain(new AgentProperties(
            "test-agent",
            "test-key",
            900_000L,
            new AgentProperties.Mcp("http://trade", "http://market", "http://news"),
            new AgentProperties.Strategy(List.of("ETH-USDC"), 35, 65, -0.3, 240, 500.0)
    ));

    @ParameterizedTest(name = "{0}")
    @MethodSource("cases")
    void evaluate(String name, double rsi, double sentiment, boolean hasPosition, StrategyBrain.Action expected) {
        var signal = BRAIN.evaluate("ETH-USDC", rsi, sentiment, hasPosition);
        assertEquals(expected, signal.action(), name);
    }

    static Stream<Arguments> cases() {
        return Stream.of(
                // LONG conditions
                arguments("RSI oversold + neutral sentiment + no position → LONG",       30.0,  0.0,  false, LONG),
                arguments("RSI oversold + positive news + no position → LONG",           25.0,  0.8,  false, LONG),
                arguments("RSI at boundary (34.9) + neutral news + no position → LONG",  34.9,  0.0,  false, LONG),

                // LONG blocked
                arguments("RSI oversold but news too bearish → HOLD",                    30.0, -0.5,  false, HOLD),
                arguments("RSI oversold but already has position → HOLD",                30.0,  0.3,  true,  HOLD),
                arguments("RSI at boundary (35.0) is not oversold → HOLD",              35.0,  0.5,  false, HOLD),

                // FLAT conditions
                arguments("RSI overbought + has position → FLAT",                        70.0,  0.9,  true,  FLAT),
                arguments("RSI at boundary (65.1) + has position → FLAT",               65.1,  0.0,  true,  FLAT),

                // FLAT blocked
                arguments("RSI overbought but no position to exit → HOLD",               70.0,  0.9,  false, HOLD),
                arguments("RSI at boundary (65.0) is not overbought → HOLD",            65.0,  0.9,  true,  HOLD),

                // Neutral zone
                arguments("RSI neutral (50) → HOLD",                                     50.0,  0.0,  false, HOLD),
                arguments("RSI neutral with position → HOLD",                            50.0,  0.5,  true,  HOLD)
        );
    }
}
