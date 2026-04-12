---
name: coverage-report
description: Rake task for analyzing test coverage from coverage/coverage.json. Group by directory, filter by pattern, check git diff coverage, shows uncovered line numbers.
---

# Coverage Report Rake Task

After running tests with SimpleCov (JSON formatter enabled), use `rails coverage:report` to analyze results.

## Prerequisites

Tests must be run first to generate `coverage/coverage.json`:

```bash
bundle exec rspec
```

If the JSON file is missing, the task will abort with instructions to run tests.

## Commands

### Full report — all files grouped by directory

```bash
rails coverage:report
```

### Filter by file name / pattern (regex)

```bash
rails 'coverage:report[worst,order]'
rails 'coverage:report[worst,payment_provider]'
rails 'coverage:report[worst,controllers/.*_controller]'
```

### Git diff modes — coverage of changed lines only

```bash
rails 'coverage:report[worst,,staged]'      # staged changes only
rails 'coverage:report[worst,,unstaged]'    # unstaged changes only
rails 'coverage:report[worst,,head]'        # last commit (HEAD~1..HEAD)
rails 'coverage:report[worst,,branch]'      # all changes since branching from main/master
```

Git mode parses `git diff` output and shows coverage **only for the lines you actually changed**. Files without changed lines in `app/` are excluded. Uncovered line numbers shown are **intersected with the diff** — only changed lines that lack test coverage are listed.

### Sort modes

```bash
rails 'coverage:report[worst]'        # all files sorted worst-first (default)
rails 'coverage:report[uncovered]'    # only 0% coverage files
rails 'coverage:report[partial]'      # only partially covered (1-99%)
rails 'coverage:report[full]'         # only 100% covered files
```

### Combine filter + sort

```bash
rails 'coverage:report[uncovered,service]'
rails 'coverage:report[partial,admin]'
```

### Combine filter + git

```bash
rails 'coverage:report[worst,order,staged]'
```

## Output Format

```
============================================================
  Coverage Report [filter: service]
  397/447 lines (88.81%)
============================================================

--- app/services (88.8% | 397/447) ---
   81.8%  app/services/asset_purchase_service.rb  (4 missed of 22)
         uncovered: 26-27, 40-41
   83.3%  app/services/tournament_resolution_service.rb  (6 missed of 36)
         uncovered: 19, 35, 66-67, 85-86
   90.7%  app/services/order_completion_service.rb  (5 missed of 54)
         uncovered: 39-40, 53-54, 125
  100.0%  app/services/order_builder.rb  (0 missed of 12)
```

Each group shows: directory name, group coverage %, covered/total lines.
Each file shows: coverage %, path, missed lines count, total relevant lines, and **uncovered line ranges**.

Consecutive uncovered lines are collapsed into ranges (`26-27` instead of `26, 27`).

In git mode, files are tagged with `[changed]` and stats reflect only changed lines. The `uncovered:` line shows only those changed lines that are not covered by tests — this is the primary signal for what needs test attention.

## Groups

Files are automatically grouped into:
- `app/controllers`, `app/models`, `app/services`, `app/helpers`
- `app/jobs`, `app/mailers`, `app/queries`, `app/channels`
- `app/concerns`, `app/views`
- `app/other` — anything else under `app/` (admin, configs, etc.)

## When to Use

- **After implementing a feature + tests**: `rails coverage:report[worst,,staged]` to verify changed lines are covered — uncovered line numbers point directly to gaps.
- **After a PR is ready**: `rails coverage:report[worst,,branch]` to review coverage of all branch changes.
- **Exploring weak spots**: `rails coverage:report[partial]` to find partially covered files with specific line numbers to target.
- **Checking specific area**: `rails 'coverage:report[partial,payment]'` to focus on a domain.
- **Finding dead code**: `rails coverage:report[uncovered]` to find completely untested files.
