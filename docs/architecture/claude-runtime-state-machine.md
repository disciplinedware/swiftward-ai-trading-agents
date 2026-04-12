# Claude Runtime - Session State Machine

The Go harness (`golang/internal/agents/claude_runtime/`) manages a Claude Code CLI process through three states.

## States

| State | Process | Claude | Timer |
|-------|---------|--------|-------|
| **Working** | alive | generating or executing tool calls | stopped |
| **Idle** | alive, stdin open | finished, waiting for input | running |
| **Killed** | not running | - | may be running |

## Key Rule: Never Close Stdin

Stdin is NEVER closed explicitly during a session. Reason: closing stdin kills the Claude process immediately, making it impossible to inject Telegram/alert messages into the Idle session. Process termination is always via context cancel -> SIGTERM (procGroupCleanup). The defer in Run() closes stdin as post-mortem cleanup only (after process is already dead).

## Idle Detection

The executor detects `result` event with `subtype: "success"` in the stream. This means Claude Code finished all work. The process stays alive, waiting for more stdin input. The harness signals `TurnDoneCh` on this event.

Stream event sequence (from actual logs):
```
user -> assistant -> user -> assistant -> ... -> assistant -> result/success -> [process stays alive]
```

## Event Table

| Event | Working | Idle | Killed |
|-------|---------|------|--------|
| **TurnDoneCh** | -> Idle, start timer, send "Session finished" to TG | impossible | impossible |
| **Timer fires** | ignore (safety) | kill (SIGTERM), start new session | start new session |
| **Telegram msg** | inject via stdin | stop timer, inject via stdin -> Working | start --continue session |
| **Alert** | inject via stdin | stop timer, inject via stdin -> Working | start --continue session |
| **`/clear`** | kill (SIGTERM), start timer | stop timer, kill (SIGTERM), start timer | start timer |
| **Process crash** | -> Killed, start timer | -> Killed, start timer | impossible |
| **Initial** | | | start first session |

- **Working**: process alive, Claude actively generating. "inject via stdin" = write to the running process.
- **Idle**: process alive, Claude done, waiting for stdin. Messages injected directly - no new process needed.
- **Killed**: process exited (crashed or killed by timer/clear). `--continue` starts a new process with prior conversation history. Seamless to user - no "Session started" announcement.
- Telegram and Alert behave identically in all states.
- `/clear` resets conversation history (forceNewSession), next session starts fresh (no `--continue`). If the next trigger is a Telegram message, it becomes the prompt directly. If timer or alert, standard prompt is used.

## Telegram Session Notifications

- On Working -> Idle (TurnDoneCh): send "Session finished, next at HH:MM UTC", store message ID.
- On any resume (timer, telegram, alert): delete the "Session finished" message.
- `--continue` sessions (telegram, alert in Killed state): NO "Session started" announcement.
- Fresh sessions (timer-triggered): show "Session #N started".

## Timer Invariant

The interval timer runs ONLY while Idle:
- **Started** on TurnDoneCh (Working -> Idle)
- **Stopped** on inject (Idle -> Working) or process crash (Idle -> Killed)

## Concurrency

4 goroutines: main loop, background session, drainTelegram, Telegram polling.

- `intervalTimer` owned exclusively by main goroutine
- `drainTelegram` signals via `tgWake` channel; main loop calls `stopTimer()`
- Shared state via atomics (`sessionActive`, `clearRequested`, `recycleRequested`), mutexes (`tgMu`, `sessionCancelMu`), channels (`sessionDoneCh`, `tgWake`, `TurnDoneCh`)

## Error Handling

- `/clear` kills do NOT count as errors (`clearRequested` flag)
- Real errors (crashes, auth failures) increment `consecutiveErrors`
- After `maxConsecutiveErrors` (default 3), the agent halts
