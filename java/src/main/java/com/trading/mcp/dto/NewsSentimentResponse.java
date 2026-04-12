package com.trading.mcp.dto;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/**
 * Response from news/get_sentiment.
 * Score range: -1.0 (very bearish) to 1.0 (very bullish).
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public record NewsSentimentResponse(
        String query,
        String sentiment,
        double score,
        @JsonProperty("article_count") int articleCount,
        @JsonProperty("key_themes") List<String> keyThemes,
        String period,
        String source
) {}
