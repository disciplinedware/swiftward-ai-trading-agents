import textwrap
from pathlib import Path

import pytest

from common.config import AgentConfig, _reset_config, get_config
from common.exceptions import ConfigError

FIXTURES = Path(__file__).parent.parent / "fixtures"
VALID_CONFIG = FIXTURES / "config_valid.yaml"


@pytest.fixture(autouse=True)
def reset_cache():
    """Clear config cache before and after each test."""
    _reset_config()
    yield
    _reset_config()


@pytest.fixture
def set_config_path(monkeypatch):
    def _set(path: Path):
        monkeypatch.setenv("AGENT_CONFIG_PATH", str(path))

    return _set


def test_valid_config_loads(set_config_path):
    set_config_path(VALID_CONFIG)
    config = get_config()
    assert isinstance(config, AgentConfig)
    assert config.trading.mode == "paper"
    assert config.assets.stablecoin == "USDC"


def test_second_call_returns_cached_instance(set_config_path):
    set_config_path(VALID_CONFIG)
    config1 = get_config()
    config2 = get_config()
    assert config1 is config2


def test_reset_forces_reload(set_config_path):
    set_config_path(VALID_CONFIG)
    config1 = get_config()
    _reset_config()
    config2 = get_config()
    assert config1 is not config2


def test_file_not_found_raises_config_error(set_config_path):
    set_config_path(Path("/nonexistent/path/config.yaml"))
    with pytest.raises(ConfigError, match="not found"):
        get_config()


@pytest.mark.parametrize(
    "name,bad_yaml,match",
    [
        (
            "missing required field",
            textwrap.dedent("""\
                chain:
                  chain_id: 1
                  rpc_url: https://rpc.example.com
                  risk_router_address: "0x01"
            """),
            "validation failed",
        ),
        (
            "wrong type for int field",
            # trigger.cooldown_minutes must be int
            None,  # built dynamically below
            "validation failed",
        ),
    ],
)
def test_invalid_config_raises_config_error(tmp_path, set_config_path, name, bad_yaml, match):
    if bad_yaml is None:
        # Load valid config, corrupt one field
        import yaml

        raw = yaml.safe_load(VALID_CONFIG.read_text())
        raw["trigger"]["cooldown_minutes"] = "not-an-int"
        bad_yaml = yaml.dump(raw)

    cfg_file = tmp_path / "config.yaml"
    cfg_file.write_text(bad_yaml)
    set_config_path(cfg_file)
    with pytest.raises(ConfigError, match=match):
        get_config()


def test_custom_path_via_env_var(tmp_path, monkeypatch):
    import shutil

    target = tmp_path / "custom_config.yaml"
    shutil.copy(VALID_CONFIG, target)
    monkeypatch.setenv("AGENT_CONFIG_PATH", str(target))
    config = get_config()
    assert config is not None


def test_default_path_used_when_env_absent(monkeypatch, tmp_path):
    monkeypatch.delenv("AGENT_CONFIG_PATH", raising=False)
    # No config.yaml in default location → should raise ConfigError
    monkeypatch.chdir(tmp_path)
    with pytest.raises(ConfigError, match="not found"):
        get_config()
