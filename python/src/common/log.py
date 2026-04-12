import logging
import os
import sys
import time
from pathlib import Path
from typing import IO

import structlog
from opentelemetry._logs import SeverityNumber
from opentelemetry._logs._internal import LogRecord
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor

from common.config import AgentConfig
from common.exceptions import ConfigError

_VALID_FORMATS = ("console", "json")

_log_file: IO[str] | None = None
_log_provider: LoggerProvider | None = None

_SEVERITY_MAP: dict[str, SeverityNumber] = {
    "debug": SeverityNumber.DEBUG,
    "info": SeverityNumber.INFO,
    "warning": SeverityNumber.WARN,
    "warn": SeverityNumber.WARN,
    "error": SeverityNumber.ERROR,
    "critical": SeverityNumber.FATAL,
}


class _TeeWriter:
    """Writes to multiple streams simultaneously."""

    def __init__(self, *streams: IO[str]) -> None:
        self._streams = streams

    def write(self, msg: str) -> None:
        for s in self._streams:
            s.write(msg)

    def flush(self) -> None:
        for s in self._streams:
            s.flush()


class _OTLPProcessor:
    """Structlog processor that tees each log record to an OTLP LoggerProvider."""

    def __init__(self, provider: LoggerProvider) -> None:
        service_name = os.environ.get("OTEL_SERVICE_NAME", "trading-agent")
        self._logger = provider.get_logger(service_name)

    def __call__(self, _logger, method: str, event_dict: dict) -> dict:
        level = event_dict.get("level", method)
        attrs = {
            k: str(v) for k, v in event_dict.items() if k not in ("event", "level", "timestamp")
        }
        record = LogRecord(
            timestamp=time.time_ns(),
            observed_timestamp=time.time_ns(),
            severity_number=_SEVERITY_MAP.get(level.lower(), SeverityNumber.INFO),
            severity_text=level.upper(),
            body=event_dict.get("event", ""),
            attributes=attrs,
        )
        self._logger.emit(record)
        return event_dict


def _init_otlp_log_provider(endpoint: str) -> LoggerProvider:
    exporter = OTLPLogExporter(endpoint=endpoint)
    provider = LoggerProvider()
    provider.add_log_record_processor(
        BatchLogRecordProcessor(exporter, schedule_delay_millis=500)
    )
    return provider


def setup_logging(config: AgentConfig) -> None:
    """Configure structlog globally. Call once at startup after get_config()."""
    global _log_file, _log_provider

    fmt = config.logging.format
    if fmt not in _VALID_FORMATS:
        raise ConfigError(f"Invalid logging format: {fmt!r}. Must be one of {_VALID_FORMATS}")

    level = getattr(logging, config.logging.level.upper(), logging.INFO)
    logging.basicConfig(level=level)

    if fmt == "json":
        renderer = structlog.processors.JSONRenderer()
    else:
        renderer = structlog.dev.ConsoleRenderer()

    if config.logging.file:
        Path(config.logging.file).parent.mkdir(parents=True, exist_ok=True)
        _log_file = open(config.logging.file, "a", encoding="utf-8")  # noqa: SIM115
        writer = _TeeWriter(sys.stdout, _log_file)
    else:
        writer = sys.stdout  # type: ignore[assignment]

    processors: list = [
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.stdlib.add_log_level,
        structlog.processors.StackInfoRenderer(),
        structlog.processors.ExceptionRenderer(),
    ]

    otlp_endpoint = config.logging.otlp_endpoint
    if otlp_endpoint:
        try:
            _log_provider = _init_otlp_log_provider(otlp_endpoint)
            processors.append(_OTLPProcessor(_log_provider))
        except Exception as exc:
            print(f"Warning: failed to init OTLP log provider: {exc}", file=sys.stderr)

    processors.append(renderer)

    structlog.configure(
        processors=processors,
        wrapper_class=structlog.make_filtering_bound_logger(level),
        logger_factory=structlog.WriteLoggerFactory(file=writer),  # type: ignore[arg-type]
        cache_logger_on_first_use=True,
    )


def shutdown_log_provider() -> None:
    """Flush and shut down the OTLP log provider on graceful shutdown."""
    if _log_provider is not None:
        _log_provider.shutdown()


def get_logger(name: str) -> structlog.BoundLogger:
    """Return a structlog logger bound with the given name."""
    return structlog.get_logger(name)
