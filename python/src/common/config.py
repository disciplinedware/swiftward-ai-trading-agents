import os
from pathlib import Path
from typing import Literal

import yaml
from pydantic import BaseModel, ValidationError

from common.exceptions import ConfigError

_config: "AgentConfig | None" = None
_DEFAULT_CONFIG_PATH = "config/config.yaml"


# --- Pydantic models ---


class ChainConfig(BaseModel):
    chain_id: int
    rpc_url: str
    risk_router_address: str


class AssetsConfig(BaseModel):
    tracked: list[str]
    stablecoin: str


class LLMConfig(BaseModel):
    base_url: str
    model: str
    api_key: str
    max_tokens: int
    retries: int


class TradingConfig(BaseModel):
    mode: Literal["paper", "live"]
    starting_balance_usdc: float
    max_concurrent_positions: int
    database_url: str
    backend: Literal["python", "go"] = "python"  # "go" = use Go Trading MCP
    agent_id: str = ""  # X-Agent-ID header for Go Trading MCP


class Stage1WeightsConfig(BaseModel):
    ema: float
    fear_greed: float
    btc_trend: float
    funding: float
    volatility: float


class Stage1Config(BaseModel):
    risk_on_threshold: float
    risk_off_threshold: float
    btc_trend_clamp_pct: float
    ema_steepness: float
    funding_peak_rate: float
    funding_extreme_rate: float
    macro_penalty_factor: float
    weights: Stage1WeightsConfig


class Stage2WeightsConfig(BaseModel):
    momentum: float
    relative_strength: float
    volume: float


class Stage2Config(BaseModel):
    weights: Stage2WeightsConfig
    held_asset_bonus: float
    max_selections: int


class RegimeMultipliersConfig(BaseModel):
    STRONG_UPTREND: float
    BREAKOUT: float
    RANGING: float
    WEAK_MIXED: float


class RegimeSlTpConfig(BaseModel):
    sl_mult: float
    tp_mult: float


class Stage3Config(BaseModel):
    half_kelly_fraction: float
    min_reward_risk_ratio: float
    regime_multipliers: RegimeMultipliersConfig
    regime_sl_tp: dict[str, RegimeSlTpConfig]


class BrainConfig(BaseModel):
    implementation: str
    stage1: Stage1Config
    stage2: Stage2Config
    stage3: Stage3Config


class TriggerConfig(BaseModel):
    cooldown_minutes: int
    price_spike_threshold_pct: float
    fear_greed_low: int = 20
    fear_greed_high: int = 80


class MCPServersConfig(BaseModel):
    price_feed_url: str
    news_url: str
    onchain_data_url: str
    fear_greed_url: str
    trading_url: str
    go_trading_url: str = ""  # e.g. "http://trading-server:8091"


class ERC8004Config(BaseModel):
    identity_registry_address: str
    validation_registry_address: str
    agent_wallet_private_key: str
    ipfs_provider: Literal["pinata", "web3storage", "mock"]
    ipfs_api_key: str


class NewsLLMConfig(BaseModel):
    base_url: str
    model: str
    api_key: str
    max_tokens: int


class ExternalAPIsConfig(BaseModel):
    binance_api_key: str = ""
    coinglass_api_key: str
    cryptopanic_api_key: str
    coindesk_api_key: str = ""


class CacheConfig(BaseModel):
    redis_url: str


class LoggingConfig(BaseModel):
    level: str
    format: Literal["console", "json"]
    file: str = ""         # optional path; if set, logs are tee'd to this file alongside stdout
    otlp_endpoint: str = ""  # if set, logs are exported via OTLP (e.g. http://signoz-otel-collector:4318/v1/logs)


class AgentConfig(BaseModel):
    chain: ChainConfig
    assets: AssetsConfig
    llm: LLMConfig
    news_llm: NewsLLMConfig
    trading: TradingConfig
    trigger: TriggerConfig
    brain: BrainConfig
    mcp_servers: MCPServersConfig
    erc8004: ERC8004Config
    external_apis: ExternalAPIsConfig
    cache: CacheConfig
    logging: LoggingConfig


# --- Public API ---


def get_config() -> AgentConfig:
    """Load, validate, and cache config from YAML. Raises ConfigError on failure."""
    global _config
    if _config is not None:
        return _config
    _config = _load_config()
    return _config


def _reset_config() -> None:
    """Clear the cached config. For tests only."""
    global _config
    _config = None


def _load_config() -> AgentConfig:
    path = Path(os.environ.get("AGENT_CONFIG_PATH", _DEFAULT_CONFIG_PATH))
    if not path.exists():
        raise ConfigError(f"Config file not found: {path}")
    try:
        with path.open() as f:
            raw = yaml.safe_load(f)
    except yaml.YAMLError as e:
        raise ConfigError(f"Failed to parse config YAML: {e}") from e
    try:
        return AgentConfig.model_validate(raw)
    except ValidationError as e:
        raise ConfigError(f"Config validation failed:\n{e}") from e
