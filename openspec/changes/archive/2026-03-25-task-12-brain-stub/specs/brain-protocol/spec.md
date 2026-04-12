## ADDED Requirements

### Requirement: Brain protocol definition
The system SHALL define a `Brain` typing.Protocol in `src/agent/brain/base.py` with a single async method `run(self, signal_bundle: SignalBundle) -> list[TradeIntent]`. The protocol SHALL be decorated with `@runtime_checkable`.

#### Scenario: Protocol satisfied by any class with matching method
- **WHEN** a class defines `async def run(self, signal_bundle: SignalBundle) -> list[TradeIntent]`
- **THEN** `isinstance(instance, Brain)` SHALL return True without the class importing or inheriting from `Brain`

#### Scenario: Protocol not satisfied by wrong return type signature
- **WHEN** a class defines `run` with a different signature (e.g., sync or wrong arguments)
- **THEN** the class SHALL NOT satisfy the Brain protocol structurally

### Requirement: Brain factory
The system SHALL provide `make_brain(config: AgentConfig) -> Brain` in `src/agent/brain/factory.py`. It SHALL read `config.brain.implementation` and return the correct `Brain` instance.

#### Scenario: Factory returns StubBrain for "stub"
- **WHEN** `config.brain.implementation` is `"stub"`
- **THEN** `make_brain(config)` SHALL return an instance of `StubBrain`

#### Scenario: Factory raises ConfigError for unknown implementation
- **WHEN** `config.brain.implementation` is any unrecognised string
- **THEN** `make_brain(config)` SHALL raise `ConfigError` with a descriptive message listing valid options

### Requirement: Brain config implementation field
`BrainConfig` in `src/common/config.py` SHALL include an `implementation: str` field. `config/config.example.yaml` SHALL include `brain.implementation: stub`.

#### Scenario: Config loads with implementation field
- **WHEN** `config.yaml` contains `brain.implementation: stub`
- **THEN** `get_config().brain.implementation` SHALL equal `"stub"`
