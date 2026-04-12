package com.trading.config;

import org.springframework.boot.context.properties.ConfigurationProperties;

import java.util.List;

@ConfigurationProperties(prefix = "agent")
public record AgentProperties(
        String id,
        String apiKey,
        long tickIntervalMs,
        Mcp mcp,
        Strategy strategy
) {

    public record Mcp(
            String tradingUrl,
            String marketUrl,
            String newsUrl
    ) {}

    public record Strategy(
            List<String> assets,
            int rsiOversold,
            int rsiOverbought,
            double newsSentimentFloor,
            int cooldownMinutes,
            double tradeValueUsd
    ) {}
}
