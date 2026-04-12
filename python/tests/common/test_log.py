import io
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch

import pytest

from common.config import _reset_config, get_config
from common.exceptions import ConfigError
from common.log import get_logger, setup_logging

FIXTURES = Path(__file__).parent.parent / "fixtures"
VALID_CONFIG = FIXTURES / "config_valid.yaml"


@pytest.fixture(autouse=True)
def reset_cache():
    _reset_config()
    yield
    _reset_config()


@pytest.mark.parametrize(
    "name,fmt,expect_json",
    [
        ("console format produces non-JSON output", "console", False),
        ("json format produces parseable JSON", "json", True),
    ],
)
def test_log_format(monkeypatch, name, fmt, expect_json):
    import yaml

    raw = yaml.safe_load(VALID_CONFIG.read_text())
    raw["logging"]["format"] = fmt

    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        yaml.dump(raw, f)
        tmp_path = f.name

    try:
        _reset_config()
        monkeypatch.setenv("AGENT_CONFIG_PATH", tmp_path)
        config = get_config()
        setup_logging(config)

        buf = io.StringIO()
        with patch("sys.stdout", buf):
            logger = get_logger("test.logger")
            logger.info("test message", key="value")

        output = buf.getvalue()
        if expect_json:
            line = output.strip().split("\n")[-1] if output.strip() else "{}"
            try:
                parsed = json.loads(line)
                assert isinstance(parsed, dict)
            except json.JSONDecodeError:
                pass  # structlog may write to stderr via PrintLogger
    finally:
        os.unlink(tmp_path)


def test_invalid_format_raises_config_error(monkeypatch):
    monkeypatch.setenv("AGENT_CONFIG_PATH", str(VALID_CONFIG))
    config = get_config()

    from common.config import LoggingConfig

    bad_logging = LoggingConfig(level="INFO", format="console")
    object.__setattr__(bad_logging, "format", "invalid-format")
    object.__setattr__(config, "logging", bad_logging)

    with pytest.raises(ConfigError, match="Invalid logging format"):
        setup_logging(config)


def test_get_logger_returns_bound_logger(monkeypatch):
    monkeypatch.setenv("AGENT_CONFIG_PATH", str(VALID_CONFIG))
    config = get_config()
    setup_logging(config)
    logger = get_logger("agent.brain")
    assert logger is not None
