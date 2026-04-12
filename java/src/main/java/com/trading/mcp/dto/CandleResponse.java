package com.trading.mcp.dto;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;
import java.util.OptionalDouble;

@JsonIgnoreProperties(ignoreUnknown = true)
public record CandleResponse(
        String market,
        String interval,
        int count,
        List<Candle> candles
) {

    @JsonIgnoreProperties(ignoreUnknown = true)
    public record Candle(
            String t,   // timestamp ISO8601
            String o,   // open
            String h,   // high
            String l,   // low
            String c,   // close
            String v,   // volume
            @JsonProperty("rsi_14") Double rsi14
    ) {}

    /**
     * Returns the most recent non-null RSI value.
     * RSI can be null during the warm-up period (first 14 candles).
     */
    public OptionalDouble latestRsi() {
        if (candles == null || candles.isEmpty()) {
            return OptionalDouble.empty();
        }
        for (int i = candles.size() - 1; i >= 0; i--) {
            Double rsi = candles.get(i).rsi14();
            if (rsi != null) {
                return OptionalDouble.of(rsi);
            }
        }
        return OptionalDouble.empty();
    }
}
