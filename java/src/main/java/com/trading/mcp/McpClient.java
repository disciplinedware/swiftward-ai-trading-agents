package com.trading.mcp;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.trading.config.AgentProperties;
import com.trading.mcp.dto.McpResponse;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestClient;

import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * JSON-RPC 2.0 client for MCP servers.
 * Uses Spring RestClient (sync) — safe with Java 21 virtual threads.
 * All MCP servers share the same protocol; only the URL and tool name differ.
 */
@Component
public class McpClient {

    private static final Logger log = LoggerFactory.getLogger(McpClient.class);

    private final RestClient restClient;
    private final AgentProperties props;
    private final ObjectMapper mapper;
    private final AtomicInteger seq = new AtomicInteger(1);

    public McpClient(AgentProperties props, ObjectMapper mapper) {
        this.props = props;
        this.mapper = mapper;
        this.restClient = RestClient.builder()
                .defaultHeader("Content-Type", "application/json")
                .defaultHeader("X-Agent-ID", props.id())
                .defaultHeader("X-Api-Key", props.apiKey())
                .build();
    }

    /**
     * Call a tool on an MCP server and deserialize the text content into responseType.
     *
     * @param mcpUrl       target MCP endpoint (e.g. http://swiftward-server:8095/mcp/market)
     * @param toolName     namespaced tool name (e.g. "market/get_candles")
     * @param arguments    tool arguments (serialized as JSON object)
     * @param responseType class to deserialize the text content into
     */
    public <T> T call(String mcpUrl, String toolName, Object arguments, Class<T> responseType) {
        var body = Map.of(
                "jsonrpc", "2.0",
                "id", seq.getAndIncrement(),
                "method", "tools/call",
                "params", Map.of("name", toolName, "arguments", arguments)
        );

        log.debug("MCP call: {} -> {}", mcpUrl, toolName);

        McpResponse response = restClient.post()
                .uri(mcpUrl)
                .body(body)
                .retrieve()
                .body(McpResponse.class);

        if (response == null) {
            throw new McpException("Null response from " + toolName);
        }
        if (response.error() != null) {
            throw new McpException("MCP error from %s: [%d] %s"
                    .formatted(toolName, response.error().code(), response.error().message()));
        }
        if (response.result() == null
                || response.result().content() == null
                || response.result().content().isEmpty()) {
            throw new McpException("Empty result content from " + toolName);
        }

        String text = response.result().content().get(0).text();
        try {
            return mapper.readValue(text, responseType);
        } catch (JsonProcessingException e) {
            throw new McpException("Failed to parse response from %s: %s".formatted(toolName, e.getMessage()), e);
        }
    }

    /**
     * Convenience overload with a pre-built arguments list for tools that accept list params.
     */
    public <T> T call(String mcpUrl, String toolName, Map<String, Object> arguments, Class<T> responseType) {
        return call(mcpUrl, toolName, (Object) arguments, responseType);
    }
}
