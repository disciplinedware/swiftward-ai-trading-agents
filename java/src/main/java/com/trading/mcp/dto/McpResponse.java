package com.trading.mcp.dto;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import java.util.List;

@JsonIgnoreProperties(ignoreUnknown = true)
public record McpResponse(
        String jsonrpc,
        Integer id,
        Result result,
        McpError error
) {

    @JsonIgnoreProperties(ignoreUnknown = true)
    public record Result(List<Content> content) {}

    @JsonIgnoreProperties(ignoreUnknown = true)
    public record Content(String type, String text) {}

    @JsonIgnoreProperties(ignoreUnknown = true)
    public record McpError(int code, String message) {}
}
