class AgentError(Exception):
    """Base exception for all agent errors."""


class ConfigError(AgentError):
    """Config loading or validation failure."""


class MCPError(AgentError):
    """MCP server communication failure."""
