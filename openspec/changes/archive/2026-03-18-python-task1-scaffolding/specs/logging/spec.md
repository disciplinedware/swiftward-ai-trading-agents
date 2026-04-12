## ADDED Requirements

### Requirement: Structured logging via structlog
The system SHALL use structlog for all logging. `get_logger(name)` SHALL return a structlog logger bound with the given name. No component SHALL use `print()`, `logging.getLogger()`, or any other logging mechanism.

#### Scenario: Logger is bound with caller name
- **WHEN** `get_logger("agent.brain")` is called
- **THEN** all log entries emitted by the returned logger include `logger="agent.brain"`

#### Scenario: Log entry includes timestamp and level
- **WHEN** any log method is called (`.info()`, `.warning()`, `.error()`)
- **THEN** the emitted entry includes `timestamp` and `level` fields

### Requirement: Format toggled by config
The system SHALL support two output formats selected by `config.logging.format`:
- `console` — human-readable, colored output via `structlog.dev.ConsoleRenderer`
- `json` — one JSON object per line via `structlog.processors.JSONRenderer`

#### Scenario: Console format produces human-readable output
- **WHEN** `config.logging.format` is `"console"`
- **THEN** log output is human-readable plain text (not JSON)

#### Scenario: JSON format produces one JSON object per line
- **WHEN** `config.logging.format` is `"json"`
- **THEN** each log entry is a valid JSON object on a single line

#### Scenario: Invalid format raises ConfigError
- **WHEN** `config.logging.format` is any value other than `"console"` or `"json"`
- **THEN** logger initialization raises `ConfigError`

### Requirement: Logger is initialized once at startup
The system SHALL configure structlog globally once via `setup_logging(config)`. Subsequent calls to `get_logger()` SHALL work without re-configuring structlog.

#### Scenario: setup_logging called once before any get_logger calls
- **WHEN** `setup_logging(config)` is called at agent startup
- **THEN** all subsequent `get_logger()` calls return correctly configured loggers
