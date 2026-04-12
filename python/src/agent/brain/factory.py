from agent.brain.base import Brain
from common.config import AgentConfig
from common.exceptions import ConfigError

_VALID = ("stub", "deterministic_llm")


def make_brain(config: AgentConfig) -> Brain:
    impl = config.brain.implementation
    if impl == "stub":
        from agent.brain.stub import StubBrain
        return StubBrain()
    if impl == "deterministic_llm":
        from agent.brain.deterministic_llm import DeterministicLLMBrain
        return DeterministicLLMBrain(config)
    raise ConfigError(
        f"Unknown brain.implementation: '{impl}'. Valid options: {', '.join(_VALID)}"
    )
