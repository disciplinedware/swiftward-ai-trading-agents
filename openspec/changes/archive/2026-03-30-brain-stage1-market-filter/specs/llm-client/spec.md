## ADDED Requirements

### Requirement: LLMClient class
The system SHALL provide an `LLMClient` class at `src/agent/brain/llm_client.py` that wraps the `openai.AsyncOpenAI` SDK. It SHALL be configured from `config.llm` (`base_url`, `model`, `api_key`, `max_tokens`, `retries`).

#### Scenario: Client instantiation from config
- **WHEN** `LLMClient(config.llm)` is called
- **THEN** an `AsyncOpenAI` client is created with the configured `base_url` and `api_key`

### Requirement: call() method
`LLMClient` SHALL expose an `async def call(system: str, user: str) -> tuple[str, dict]` method that:
1. Calls `chat.completions.create()` with the configured model and max_tokens
2. Applies character encoding normalization to the raw response text
3. Parses the XML+JSON envelope: extracts `<reasoning>` text and `<decision>` JSON
4. Returns `(reasoning: str, decision: dict)`

#### Scenario: Successful call returns reasoning and decision
- **WHEN** the LLM responds with a valid XML+JSON envelope
- **THEN** `call()` returns `(reasoning_text, {"verdict": "RISK_ON", "reason": "..."})`

#### Scenario: Missing reasoning tag → empty string
- **WHEN** the LLM response has no `<reasoning>` tag
- **THEN** the returned reasoning is `""` and no exception is raised

### Requirement: XML+JSON 4-level fallback parsing
The `LLMClient.call()` method SHALL extract the JSON decision using a 4-level fallback chain:
1. Content inside `<decision>` tag (strip markdown code fence if present)
2. Text starting from the first `[` character (for array responses)
3. Text starting from the first `{` character
4. Full response text

The first level that successfully parses as JSON SHALL be used.

#### Scenario: Decision in <decision> tag parsed
- **WHEN** response contains `<decision>```json{"verdict":"RISK_ON"}```</decision>`
- **THEN** decision dict is `{"verdict": "RISK_ON"}`

#### Scenario: Fallback to { when no tag present
- **WHEN** response contains no `<decision>` tag but contains `{"verdict":"UNCERTAIN"}`
- **THEN** decision dict is `{"verdict": "UNCERTAIN"}`

#### Scenario: All fallbacks fail → raises MCPError
- **WHEN** the response contains no parseable JSON
- **THEN** `MCPError` is raised with a descriptive message

### Requirement: Character encoding normalization
Before `json.loads()`, the `LLMClient` SHALL normalize Chinese/fullwidth characters to ASCII equivalents:
- Chinese quotes `\u201c\u201d` → `"`
- Fullwidth brackets `\uff3b\uff3d` → `[]`
- Fullwidth braces `\uff5b\uff5d` → `{}`
- Fullwidth colon `\uff1a` → `:`
- Fullwidth comma `\uff0c` → `,`

#### Scenario: Chinese quotes normalized before JSON parse
- **WHEN** LLM response contains `\u201cverdict\u201d:\u201cRISK_ON\u201d`
- **THEN** `json.loads()` succeeds and returns `{"verdict": "RISK_ON"}`

### Requirement: Retry with exponential backoff
`LLMClient.call()` SHALL retry up to `config.llm.retries` times on transient errors (timeout, connection error, 5xx). Backoff SHALL be `2 ** attempt` seconds (0s, 2s, 4s for 3 retries). After all retries are exhausted, the final exception SHALL be re-raised.

#### Scenario: Succeeds on second attempt after timeout
- **WHEN** the first call raises a timeout and the second succeeds
- **THEN** the method returns the successful result without raising

#### Scenario: All retries exhausted → raises
- **WHEN** all `config.llm.retries` attempts fail
- **THEN** the exception is re-raised to the caller
