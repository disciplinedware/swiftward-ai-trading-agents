package com.trading.mcp.dto;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * Response from trade/submit_order.
 * status = "fill"   → order executed, see fill field
 * status = "reject" → blocked by policy or agent halted, see reject field
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public record TradeResponse(
        String status,
        Fill fill,
        Reject reject,
        @JsonProperty("decision_hash") String decisionHash
) {

    @JsonIgnoreProperties(ignoreUnknown = true)
    public record Fill(
            String id,
            String pair,
            String side,
            String price,
            String qty,
            String value
    ) {}

    @JsonIgnoreProperties(ignoreUnknown = true)
    public record Reject(
            String source,
            String reason
    ) {}

    public boolean isFilled()   { return "fill".equals(status); }
    public boolean isRejected() { return "reject".equals(status); }
}
