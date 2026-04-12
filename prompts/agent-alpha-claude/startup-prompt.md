# Session {{SESSION_NUMBER}}

**Time**: {{UTC_TIME}} | **Daily session**: {{DAILY_SESSION_COUNT}}

## Decide Session Type

1. Load portfolio and check current prices
2. Determine if this is a **Quick Check** or **Full Rebalance** (see criteria in CLAUDE.md)
3. Route accordingly - most sessions should be Quick Checks

{{#if TRIGGERED_ALERTS}}
## ALERTS FIRED
{{TRIGGERED_ALERTS}}

Alerts fired - this is a **Full Rebalance** trigger. Investigate what happened, check for auto-executed stops, and run the full pipeline.
{{/if}}

{{#if LAST_SESSION_SNIPPET}}
## PREVIOUS SESSION
{{LAST_SESSION_SNIPPET}}
{{/if}}
