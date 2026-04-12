## 1. Progress tracking

- [x] 1.1 Mark Task 20 as `in_progress` in `python/docs/progress.md`

## 2. Config

- [x] 2.1 Add `brain.tier2.fear_greed_low: 20` and `brain.tier2.fear_greed_high: 80` to `python/config/config.example.yaml`
- [x] 2.2 Add `Tier2Config` model with `fear_greed_low: int` and `fear_greed_high: int` fields to `src/common/config.py`; nest under `BrainConfig.tier2`

## 3. Core implementation

- [x] 3.1 Create `python/src/agent/trigger/tier2.py` with `Tier2Loop` class (5-min interval, F&G + news checks, thresholds from config, single `_run_once()` call per cycle)
- [x] 3.2 Update `python/src/agent/main.py` — replace `_noop_loop()` with `Tier2Loop` wired with `fear_greed`, `news`, `gate`, `clock`, and `config`

## 4. Tests

- [x] 4.1 Create `python/tests/agent/trigger/test_tier2.py` covering: F&G low/high thresholds, boundary values, macro flag trigger, both conditions true fires once, no conditions no brain call, fetch error skips cycle

## 5. Wrap-up

- [x] 5.1 Run `make lint` and `make test` — all pass
- [x] 5.2 Mark Task 20 as `done` in `python/docs/progress.md`
