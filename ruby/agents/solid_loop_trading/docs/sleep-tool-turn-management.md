# Trading Agent Turn Management: sleep() as the End of a Step

---

## Problem: How the Agent Signals It Has Finished Thinking

When an LLM manages a trading portfolio in step-by-step simulation mode, the system faces a fundamental question: **when is a turn considered complete?**

The classic answer — the orchestrator tracks message patterns. If the last message from the model contains no tool calls, it has "finished". This works, but is fragile: the turn boundary is implicit, the detection logic lives in custom orchestrator code, and there is no verification that the model actually did what it planned.

The alternative — an explicit turn-completion tool.

---

## Trading Session and Turn

A **trading session** is a virtual account with fixed parameters: time range, simulation step, starting balance. Time inside is simulated. All accounting goes through orders and double-entry bookkeeping. A session lives from the first deposit to the final liquidation.

A **turn** is one decision-making cycle:

```
[market update] → analysis → tool calls → turn completion
```

The model receives a market snapshot, analyzes the situation, places orders — and explicitly declares the turn complete.

---

## The Smart Model Mistake

A smart model can make the classic trader's mistake: **focus on analysis and forget about execution**.

A real example from a log:

> Message 640: *"The ETH sell order never executed — I reasoned about selling but forgot to actually place the order."*

> Message 644 (turn summary): *"SOLD ALL ETH @ $1,986"* — but `add_order` was never called.

> Message 645 (next market update): portfolio — `ETHUSD: available: 22.05`. ETH is still there.

This is not a model bug in the usual sense. The model tuned its attention to trading — analyzing candles, news, fear indexes. The tool call — an operational detail that "fell through the cracks". Without architectural protection, such errors are invisible: the turn ended, the orchestrator advanced time, the model continued with the wrong portfolio state.

---

## sleep() as an Explicit Turn End

Instead of guessing — a tool:

```json
{
  "name": "sleep",
  "description": "End the current turn and proceed to the next one after the specified time",
  "parameters": {
    "duration": "number of seconds until the next turn",
    "summary": "brief description of what was done in this turn"
  }
}
```

Now the turn only ends when the model explicitly calls `sleep`. The boundary is guaranteed, not guessed. The `duration` parameter carries trading meaning: the model decides to wait one hour or eight hours — this is part of the strategy. The `summary` parameter provides a natural verification point.

---

## Swiftward: Verification at the sleep Entry Point

Swiftward is an interceptor that checks the legality of a tool call before executing it. When `sleep` is called, it has the full turn context: all messages, all tools called, the contents of `summary`.

The simplest check — compare the claims in the summary against actual tool calls:

```
Model wrote in summary: "sold ETH"
Swiftward looks for: add_order(side=sell, asset=ETH) in turn's tool_calls
Not found → rejects sleep:
  "sleep rejected: summary claims ETH was sold, but add_order was never called.
   Execute the trade or explain why the decision changed."
```

The model receives the rejection directly in the tool result and continues the current turn — it does not wait for the next market update. Swiftward does not need to be complex: even a small specialized model focused on finding discrepancies between text and actions will handle this more reliably than general logic in the orchestrator.

---

## Simplifying the Orchestrator

What the orchestrator does today without sleep:

- Detects turn end by message pattern
- Compactifies message history
- Advances virtual time
- Builds the next market update
- Injects it into the session
- Tracks the final boundary

All of this is custom code tied to a specific agent framework.

With `sleep(duration)`:

- Turn end is detected by MCP (received a sleep call)
- Time advancement — inside the MCP handler
- Market update — built and returned by MCP as the sleep response
- Final liquidation — when `current_time + duration >= end_at`, MCP returns a special response

The orchestrator becomes thin. The logic lives in MCP — and this matters: **switching agent frameworks does not break trading logic**. MCP is a standard; orchestrators vary.

---

## sleep Implementation: Two Options

### Option 1: Synchronous — simple, works in Claude CLI

```python
@mcp_tool
def sleep(duration: int, summary: str) -> str:
    # Swiftward has already verified summary before this point
    advance_virtual_time(duration)
    next_update = build_market_update()
    return f"Time advanced by {duration}s.\n\n{next_update}"
```

MCP holds the connection, does its work, returns the response. Claude CLI called the tool — waited — received the market update — continued the turn.

**Works:** in backtesting, short sessions, when Claude CLI connects directly to MCP.

**Does not work reliably:** in live trading with real pauses — HTTP connections time out after minutes. Also no resilience to restarts: MCP crash while waiting = lost context.

---

### Option 2: Async with resume — for live trading

`sleep()` immediately registers the event and returns a response:

```python
@mcp_tool
def sleep(duration: int, summary: str) -> str:
    register_wakeup(
        session_id=current_session_id(),
        wake_at=now() + duration,
        price_triggers=extract_triggers(summary),  # if the model mentioned price levels
    )
    return "Registered. The session will resume on the event."
```

Claude CLI ends the session. No open connection.

When the event occurs (timer, price alert, stop-loss), MCP or an external service sends a callback to the orchestrator:

```json
POST /orchestrator/wake
{
  "session_id": "abc123",
  "reason": "price_trigger",
  "data": { "asset": "BTC", "price": 62800, "trigger": "below_63k" }
}
```

The orchestrator starts a new turn:

```bash
claude --resume abc123 \
  --message "Stop-loss triggered: BTC dropped to $62,800"
```

Or via the framework:

```ruby
solid_loop.resume!(
  user_message: "Stop-loss triggered: BTC = $62,800"
)
```

**The model does not know the difference** between backtesting and live trading. It called `sleep` — received the wake reason in the next message. All the difference is inside MCP.

---

## MCP as an Event Hub in Live Trading

In backtesting, MCP simulates time. In live trading, the same MCP becomes an event manager:

```
sleep() registered:
├── wake_at: in 4 hours
├── price_down: if BTC < $63,000
└── price_up: if BTC > $70,000

First to trigger → wake with reason → new agent turn
```

The agent receives specifics: not just "time has passed", but "a price trigger fired, here is the context". This is information for the next decision.

---

## Summary

| | Without sleep | With sleep |
|---|---|---|
| Turn boundary | Guessed by orchestrator | Explicit, guaranteed |
| Action verification | None | Swiftward at sleep entry |
| Time advancement logic | In orchestrator | In MCP |
| Framework switch | Rewrite orchestrator | MCP stays |
| Live trading | Complex | Async + resume |

`sleep()` is not just a pause. It is a contract: the model declares that it has completed the turn and takes responsibility for the summary. Swiftward verifies this contract. MCP executes its consequences.
