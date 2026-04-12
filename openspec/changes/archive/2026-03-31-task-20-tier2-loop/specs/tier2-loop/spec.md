## ADDED Requirements

### Requirement: Tier 2 loop polls every 5 minutes
The `Tier2Loop` SHALL poll its trigger conditions every 300 seconds. On each poll it checks both conditions in parallel and fires at most one brain cycle per poll regardless of how many conditions are true.

#### Scenario: Idle poll — no conditions met
- **WHEN** F&G value is between 20 and 80 inclusive AND macro news flag is False
- **THEN** no brain cycle is fired

#### Scenario: Brain fires at most once per cycle
- **WHEN** both F&G threshold crossing and macro news flag are true simultaneously
- **THEN** `clock._run_once()` is called exactly once

### Requirement: Fear & Greed threshold crossing triggers brain cycle
The loop SHALL fire a brain cycle when the Fear & Greed index value is strictly less than `brain.tier2.fear_greed_low` (extreme fear) or strictly greater than `brain.tier2.fear_greed_high` (extreme greed). Both thresholds SHALL be read from config at construction time. Default values are 20 and 80. The F&G value SHALL be read by calling `fear_greed.get_index()`, which returns the server-cached daily value without re-fetching from Alternative.me.

#### Scenario: Extreme fear triggers brain
- **WHEN** `fear_greed.get_index()` returns value strictly less than `fear_greed_low`
- **THEN** `clock._run_once()` is called

#### Scenario: Extreme greed triggers brain
- **WHEN** `fear_greed.get_index()` returns value strictly greater than `fear_greed_high`
- **THEN** `clock._run_once()` is called

#### Scenario: Boundary value equal to low threshold does not trigger
- **WHEN** `fear_greed.get_index()` returns value == `fear_greed_low` AND macro flag is False
- **THEN** `clock._run_once()` is NOT called

#### Scenario: Boundary value equal to high threshold does not trigger
- **WHEN** `fear_greed.get_index()` returns value == `fear_greed_high` AND macro flag is False
- **THEN** `clock._run_once()` is NOT called

### Requirement: Macro news flag triggers brain cycle
The loop SHALL fire a brain cycle when the news-mcp `macro_flag` field is True in any asset's `NewsData`. The flag is extracted from `news.get_signals(tracked_assets)`. The loop SHALL NOT make a separate MCP call for the macro flag.

#### Scenario: Macro flag True triggers brain
- **WHEN** `news.get_signals()` returns data with `macro_flag=True` for any asset
- **THEN** `clock._run_once()` is called

#### Scenario: Macro flag False does not trigger
- **WHEN** `news.get_signals()` returns data with `macro_flag=False` for all assets AND F&G is in range
- **THEN** `clock._run_once()` is NOT called

### Requirement: Fetch errors skip the cycle without crashing
If `fear_greed.get_index()` or `news.get_signals()` raises an exception, the loop SHALL log the error and skip the current cycle. The loop SHALL continue running on its next interval.

#### Scenario: Fear & Greed fetch fails
- **WHEN** `fear_greed.get_index()` raises an exception
- **THEN** the cycle is skipped, no brain call is made, the loop continues

#### Scenario: News fetch fails
- **WHEN** `news.get_signals()` raises an exception
- **THEN** the cycle is skipped, no brain call is made, the loop continues
