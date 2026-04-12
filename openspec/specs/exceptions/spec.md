### Requirement: Base exception hierarchy
The system SHALL define a base exception hierarchy in `src/common/exceptions.py`. All agent and MCP server code SHALL raise exceptions from this hierarchy rather than bare `Exception`.

Hierarchy:
```
AgentError          — base for all agent errors
├── ConfigError     — config loading or validation failure
└── MCPError        — MCP server communication failure
```

#### Scenario: ConfigError is catchable as AgentError
- **WHEN** a `ConfigError` is raised
- **THEN** it can be caught with `except AgentError`

#### Scenario: MCPError is catchable as AgentError
- **WHEN** an `MCPError` is raised
- **THEN** it can be caught with `except AgentError`

#### Scenario: Exceptions carry a message
- **WHEN** any exception in the hierarchy is raised with a message string
- **THEN** `str(exc)` returns that message
