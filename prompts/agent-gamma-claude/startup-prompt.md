# Trading Session Startup

**Current UTC time**: {{UTC_TIME}}
**Session number**: {{SESSION_NUMBER}} (today: {{DAILY_SESSION_COUNT}})
**Agent ID**: {{AGENT_ID}}

{{#if TRIGGERED_ALERTS}}
## ALERTS TRIGGERED (reason for this session)
{{TRIGGERED_ALERTS}}
{{/if}}

{{#if LAST_SESSION_SNIPPET}}
## LAST SESSION SUMMARY
{{LAST_SESSION_SNIPPET}}
{{/if}}

---

Run /trading-session
