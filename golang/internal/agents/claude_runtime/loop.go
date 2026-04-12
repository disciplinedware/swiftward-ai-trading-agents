// Session state machine - see docs/claude-runtime-state-machine.md for the full event table.
//
// Three states: Working (Claude active, timer stopped), Idle (process alive waiting for
// stdin, timer running), Killed (process exited). TurnDoneCh signals Working -> Idle.
// Never close stdin - process termination is always via SIGTERM (context cancel).
package claude_runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-trading-agents/internal/mcp"
	"go.uber.org/zap"
)

// sessionOutcome is sent by the background session goroutine to the main loop.
type sessionOutcome struct {
	sessionID string
	startedAt time.Time
	result    SessionResult
	fatal     bool // auth error or max consecutive errors - loop must stop
	cleared   bool // /clear killed this session - suppress "Session finished" notification
}

// Loop drives the event-driven Claude session lifecycle.
//
// Architecture: the main loop is a select-based event dispatcher. Sessions run
// in a background goroutine. The main loop reacts to: session completion, timer,
// alert polls, Telegram messages (/clear, wake), and context cancellation.
type Loop struct {
	cfg        Config
	exec       *Executor
	triageExec *Executor // lightweight model for wake_triage pre-filter
	lifecycle  *LifecycleEmitter
	log        *zap.Logger
	sessionLog *zap.Logger // loop.session - tool calls, session summaries
	alertLog   *zap.Logger // loop.alerts - alert polling and injection

	sessionNum    int
	dailySessions int
	lastReset     time.Time

	consecutiveErrors  int
	lastSessionSnippet string

	// pendingAlerts holds triggered alerts injected into the next session's prompt.
	// Accessed from main goroutine (handleAlertPoll, startSessionAsync) and
	// background goroutine (runSessionBlocking reads via passed-in copy).
	pendingAlerts []map[string]any
	triageOutput  string // Haiku triage analysis passed to expensive session, cleared after use

	// wakeup tracking for cooldown and hourly cap enforcement
	lastWakeup      time.Time
	wakeupCount     int       // wakeups in the current hour window
	wakeupHourStart time.Time

	attemptNum int // monotonically increasing attempt counter (includes triage skips)

	tradingMCP *mcp.Client // for alert polling
	marketMCP  *mcp.Client // for alert polling

	// Telegram integration
	tg              *TelegramBot
	tgWake          chan struct{}    // signal from TG goroutine to wake main loop
	tgMu            sync.Mutex      // protects pendingHumanMsg, forceNewSession
	pendingHumanMsg *TelegramMessage // message that triggered --continue session
	sessionActive   atomic.Bool

	// /clear and recycle support
	forceNewSession  bool                 // next session skips --continue
	clearRequested   atomic.Bool          // distinguishes /clear kill from real errors
	recycleRequested atomic.Bool          // timer-triggered recycle: start new session immediately
	sessionCancelMu  sync.Mutex           // protects sessionCancel
	sessionCancel    context.CancelFunc   // cancels the active session context

	// Event-driven loop state
	sessionDoneCh  chan sessionOutcome // background session sends result here
	intervalTimer  *time.Timer        // fires after cfg.Interval to start next session
	alertTicker    *time.Ticker       // periodic alert polling
	idleTgMsgID    int                // Telegram message ID of "session idle" notification (0 = none)
}

// NewLoop creates a Loop with the given config and logger.
func NewLoop(cfg Config, log *zap.Logger) *Loop {
	sessionLog := log.Named("session")
	alertLog := log.Named("alerts")

	exec := NewExecutor(log.Named("executor"))
	exec.IdleTimeout = cfg.IdleTimeout
	exec.TurnDoneCh = make(chan struct{}, 1)
	if cfg.Model != "" {
		exec.Model = cfg.Model
	}

	triageExec := NewExecutor(log.Named("triage"))
	triageExec.IdleTimeout = 2 * time.Minute // triage sessions are short
	if cfg.TriageModel != "" {
		triageExec.Model = cfg.TriageModel
	}

	now := time.Now().UTC()
	l := &Loop{
		cfg:             cfg,
		exec:            exec,
		triageExec:      triageExec,
		lifecycle:       NewLifecycleEmitter(cfg, log.Named("lifecycle")),
		log:             log,
		sessionLog:      sessionLog,
		alertLog:        alertLog,
		lastReset:       now,
		wakeupHourStart: now,
		tgWake:          make(chan struct{}, 1),
		sessionDoneCh:   make(chan sessionOutcome, 1),
	}

	// Telegram bot (optional).
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != 0 {
		tg, err := NewTelegramBot(cfg, log.Named("telegram"))
		if err != nil {
			log.Error("Failed to create Telegram bot - continuing without", zap.Error(err))
		} else {
			l.tg = tg
		}
	}

	// Wire tool handler for session activity logging.
	exec.ToolHandler = func(ev ToolEvent) {
		sessionLog.Info("Tool call: "+ev.Name,
			zap.String("input", ev.Input),
		)
	}

	// Wire output and event handlers.
	exec.OutputHandler = func(text string) {
		fmt.Print(text) // real-time terminal output
		if l.tg != nil {
			l.tg.AppendOutput(text)
		}
	}
	exec.EventHandler = func(eventType string) {
		if l.tg == nil {
			return
		}
		if eventType != "text_delta" {
			l.tg.SendTyping(context.Background())
		}
	}
	if cfg.TradingMCPURL != "" {
		l.tradingMCP = mcp.NewClient(cfg.TradingMCPURL, "", 5*time.Minute)
		l.tradingMCP.SetHeader("X-Agent-ID", cfg.AgentID)
	}
	if cfg.MarketMCPURL != "" {
		l.marketMCP = mcp.NewClient(cfg.MarketMCPURL, "", 5*time.Minute)
		l.marketMCP.SetHeader("X-Agent-ID", cfg.AgentID)
	}

	l.seedSessionCounters()
	return l
}

// Run starts the event-driven session loop and blocks until ctx is cancelled or a fatal error occurs.
func (l *Loop) Run(ctx context.Context) error {
	l.log.Info("Claude agent loop starting",
		zap.String("agent_id", l.cfg.AgentID),
		zap.Duration("interval", l.cfg.Interval),
		zap.Bool("telegram", l.tg != nil),
	)
	l.lifecycle.Emit(ctx, "started", "", nil)

	// Start Telegram bot and drain goroutine.
	if l.tg != nil {
		l.tg.Start(ctx)
		go l.drainTelegram(ctx)
	}

	// Start alert polling ticker.
	pollInterval := l.cfg.AlertPollInterval
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	l.alertTicker = time.NewTicker(pollInterval)
	defer l.alertTicker.Stop()

	// Start first session immediately.
	l.startSessionAsync(ctx)

	var fatalErr error
	for {
		select {
		case outcome := <-l.sessionDoneCh:
			fatalErr = l.handleSessionDone(ctx, outcome)
			if fatalErr != nil {
				goto done
			}

		case <-l.exec.TurnDoneCh:
			// Claude Code emitted result/success. Process stays alive (stdin open).
			// State: Working -> Idle. Start recycle timer.
			// Guard: if sessionActive is false, this is a stale signal from a
			// previous session (process died before we consumed TurnDoneCh). Discard.
			if !l.sessionActive.Load() {
				break
			}
			l.stopTimer()
			l.intervalTimer = time.NewTimer(l.cfg.Interval)
			nextAt := time.Now().Add(l.cfg.Interval).UTC().Format("15:04")
			l.sessionLog.Info("Session idle (result/success), recycle timer started")
			if l.tg != nil {
				// Flush pending output BEFORE sending "Session finished" so the
				// notification always appears after Claude's last message.
				l.tg.FlushOutput(ctx)
				l.idleTgMsgID = l.tg.SendTextReturningID(ctx,
					fmt.Sprintf("-- Session finished, next at %s UTC --", nextAt))
			}

		case <-l.timerCh():
			l.intervalTimer = nil
			l.deleteIdleMsg(ctx)
			if l.sessionActive.Load() {
				// Idle -> kill process via SIGTERM -> handleSessionDone starts new session immediately.
				l.recycleRequested.Store(true)
				l.sessionCancelMu.Lock()
				cancel := l.sessionCancel
				l.sessionCancelMu.Unlock()
				if cancel != nil {
					cancel()
				}
				l.log.Info("Recycle: killing idle session")
			} else {
				// Killed -> start new session directly.
				l.startSessionAsync(ctx)
			}

		case <-l.alertTicker.C:
			l.handleAlertPoll(ctx)

		case <-l.tgWake:
			// tgWake is sent by drainTelegram for:
			// 1. Idle + message injected -> stop timer (session handles it via stdin)
			// 2. Killed + pending msg -> start --continue session
			// 3. /clear -> restart timer for next fresh session
			if l.sessionActive.Load() {
				// Idle -> Working: cancel recycle timer, message already injected via stdin.
				l.stopTimer()
				l.deleteIdleMsg(ctx)
				continue
			}
			l.stopTimer()
			l.tgMu.Lock()
			hasPending := l.pendingHumanMsg != nil
			l.tgMu.Unlock()
			if hasPending {
				l.deleteIdleMsg(ctx)
				l.startSessionAsync(ctx)
			} else {
				// /clear: start timer for next fresh session.
				l.intervalTimer = time.NewTimer(l.cfg.Interval)
				l.log.Info("/clear: timer started for next session",
					zap.Duration("interval", l.cfg.Interval),
				)
			}

		case <-ctx.Done():
			goto done
		}
	}

done:
	l.lifecycle.EmitSync(context.Background(), "stopped", "", nil)
	l.log.Info("Claude agent loop stopped")
	return fatalErr
}

// deleteIdleMsg removes the "session finished" Telegram notification if one was sent.
func (l *Loop) deleteIdleMsg(ctx context.Context) {
	if l.tg != nil && l.idleTgMsgID != 0 {
		l.tg.DeleteMessage(ctx, l.idleTgMsgID)
		l.idleTgMsgID = 0
	}
}

// timerCh returns the interval timer channel, or nil (blocks forever) if no timer is active.
func (l *Loop) timerCh() <-chan time.Time {
	if l.intervalTimer != nil {
		return l.intervalTimer.C
	}
	return nil
}

// stopTimer cancels and clears the interval timer.
func (l *Loop) stopTimer() {
	if l.intervalTimer != nil {
		l.intervalTimer.Stop()
		l.intervalTimer = nil
	}
}

// startSessionAsync launches a session in a background goroutine.
// The main loop receives the outcome on sessionDoneCh.
func (l *Loop) startSessionAsync(ctx context.Context) {
	if l.sessionActive.Load() {
		return
	}
	// Set active synchronously on the main goroutine to prevent double-start.
	l.sessionActive.Store(true)

	// Snapshot pendingAlerts for the background goroutine (avoids concurrent access).
	alerts := l.pendingAlerts
	l.pendingAlerts = nil

	go func() {
		outcome := l.runSessionBlocking(ctx, alerts)
		l.sessionDoneCh <- outcome
		// Clear sessionActive AFTER the main loop has received the outcome.
		// Prevents a race where drainTelegram sees sessionActive=false and
		// starts a new session before handleSessionDone processes this one.
		l.sessionActive.Store(false)
	}()
}

// handleSessionDone processes a session outcome from the background goroutine.
// Returns a non-nil error only for fatal conditions (auth, max consecutive errors).
func (l *Loop) handleSessionDone(ctx context.Context, o sessionOutcome) error {
	if o.fatal {
		return fmt.Errorf("fatal session error: %s", o.sessionID)
	}

	// Timer-triggered recycle: start fresh session immediately (interval already elapsed).
	if l.recycleRequested.CompareAndSwap(true, false) {
		l.log.Info("Session recycled - starting fresh")
		// Force fresh session (not --continue) even if pendingHumanMsg is set.
		l.tgMu.Lock()
		l.pendingHumanMsg = nil
		l.tgMu.Unlock()
		l.startSessionAsync(ctx)
		return nil
	}

	// If a pending message exists, start next session after short cooldown.
	l.tgMu.Lock()
	hasPending := l.pendingHumanMsg != nil
	l.tgMu.Unlock()
	if hasPending {
		l.stopTimer()
		l.log.Info("Pending Telegram message - starting next session after short cooldown")
		time.AfterFunc(5*time.Second, func() {
			select {
			case l.tgWake <- struct{}{}:
			default:
			}
		})
		return nil
	}

	// Normal completion: schedule next session after interval.
	// "Session finished" TG message was already sent by TurnDoneCh handler (Working -> Idle).
	// If the process crashed (no TurnDoneCh), send it now.
	l.stopTimer()
	l.intervalTimer = time.NewTimer(l.cfg.Interval)
	nextAt := time.Now().Add(l.cfg.Interval)
	l.log.Info("Next session scheduled",
		zap.Duration("interval", l.cfg.Interval),
		zap.String("next_at", nextAt.UTC().Format("15:04:05")),
	)
	if l.tg != nil && l.idleTgMsgID == 0 && !o.cleared {
		// No idle message yet (crash path - TurnDoneCh never fired).
		// Skip for /clear kills - handleClear already sent its own reply.
		l.tg.FlushOutput(ctx)
		l.idleTgMsgID = l.tg.SendTextReturningID(ctx,
			fmt.Sprintf("-- Session finished, next at %s UTC --", nextAt.UTC().Format("15:04")))
	}
	return nil
}

// handleAlertPoll polls for triggered alerts and either injects them into
// the active session or accumulates them and starts a new session.
func (l *Loop) handleAlertPoll(ctx context.Context) {
	var newAlerts []map[string]any
	if l.tradingMCP != nil {
		// alert/triggered is the unified endpoint - returns all triggered alerts across services
		newAlerts = append(newAlerts, l.pollMCPAlerts(l.tradingMCP, "alert/triggered")...)
	}
	if len(newAlerts) == 0 {
		return
	}

	newAlerts = l.dedupAlerts(newAlerts)
	if len(newAlerts) == 0 {
		return
	}

	l.pendingAlerts = append(l.pendingAlerts, newAlerts...)

	if l.sessionActive.Load() {
		// Session alive (Working or Idle): inject alert data as a message.
		alertText := formatAlertInjection(newAlerts)
		if err := l.exec.InjectMessage(alertText); err != nil {
			l.alertLog.Warn("Alert inject failed, will include in next session prompt", zap.Error(err))
		} else {
			// Idle -> Working: stop recycle timer so it doesn't kill the session
			// while Claude is responding to the alert.
			l.stopTimer()
			l.deleteIdleMsg(context.Background())
			l.alertLog.Info("Alerts injected into active session", zap.Int("count", len(newAlerts)))
		}
		return
	}

	// Session dead: apply wakeup cooldown/cap before starting a new session.
	now := time.Now()
	cooldown := l.cfg.WakeupCooldown
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	maxPerHour := l.cfg.MaxWakeupsPerHour
	if maxPerHour <= 0 {
		maxPerHour = 6
	}

	if now.Sub(l.wakeupHourStart) >= time.Hour {
		l.wakeupCount = 0
		l.wakeupHourStart = now
	}

	if !l.lastWakeup.IsZero() && now.Sub(l.lastWakeup) < cooldown {
		l.alertLog.Info("Alert wakeup suppressed by cooldown",
			zap.Int("alerts", len(newAlerts)),
			zap.Duration("cooldown_remaining", cooldown-now.Sub(l.lastWakeup)),
		)
		return
	}
	if maxPerHour > 0 && l.wakeupCount >= maxPerHour {
		l.alertLog.Info("Alert wakeup suppressed by hourly cap",
			zap.Int("alerts", len(newAlerts)),
			zap.Int("cap", maxPerHour),
		)
		return
	}

	l.lastWakeup = now
	l.wakeupCount++
	l.alertLog.Info("Triggered alerts detected - starting --continue session",
		zap.Int("count", len(newAlerts)),
		zap.Int("wakeups_this_hour", l.wakeupCount),
	)
	l.stopTimer()
	l.deleteIdleMsg(ctx)
	l.startSessionAsync(ctx)
}

// formatAlertInjection formats alert data as a message for injection into a live session.
func formatAlertInjection(alerts []map[string]any) string {
	var sb strings.Builder
	sb.WriteString("[SYSTEM ALERT] Triggered alerts:\n")
	for _, a := range alerts {
		sb.WriteString(formatSingleAlert(a))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// formatSingleAlert formats one alert map into a human-readable line.
// Used by formatAlertInjection, buildPrompt, and runTriageSession.
func formatSingleAlert(a map[string]any) string {
	service, _ := a["service"].(string)
	alertType, _ := a["type"].(string)
	pair, _ := a["pair"].(string)
	market, _ := a["market"].(string)
	condition, _ := a["condition"].(string)
	note, _ := a["note"].(string)
	triggeredAt, _ := a["triggered_at"].(string)
	triggeredPrice, _ := a["triggered_price"].(string)

	if triggeredAt == "" {
		triggeredAt = "unknown time"
	}

	switch {
	case service == "time":
		fireAt, _ := a["fire_at"].(string)
		if fireAt != "" {
			return fmt.Sprintf("- [reminder] Scheduled for %s, triggered at %s. Note: %s", fireAt, triggeredAt, note)
		}
		return fmt.Sprintf("- [reminder] Triggered at %s. Note: %s", triggeredAt, note)

	case service == "news":
		parts := []string{}
		if v := anyToString(a["markets"]); v != "" {
			parts = append(parts, "markets: "+v)
		}
		if v := anyToString(a["categories"]); v != "" {
			parts = append(parts, "categories: "+v)
		}
		summary := strings.Join(parts, ", ")
		if summary == "" {
			summary = "general"
		}
		return fmt.Sprintf("- [news] %s, triggered at %s. Note: %s", summary, triggeredAt, note)

	case pair != "":
		pricePart := ""
		if triggeredPrice != "" {
			pricePart = fmt.Sprintf(" (price: %s)", triggeredPrice)
		}
		return fmt.Sprintf("- [trading] %s on %s triggered at %s%s. Note: %s", alertType, pair, triggeredAt, pricePart, note)

	case market != "":
		pricePart := ""
		if triggeredPrice != "" {
			pricePart = fmt.Sprintf(" (price: %s)", triggeredPrice)
		}
		return fmt.Sprintf("- [market] Price alert (%s) on %s triggered at %s%s. Note: %s", condition, market, triggeredAt, pricePart, note)

	default:
		var parts []string
		for k, v := range a {
			if k == "alert_id" || k == "on_trigger" || k == "status" || k == "triage_prompt" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
		sort.Strings(parts)
		return fmt.Sprintf("- [%s] %s", service, strings.Join(parts, " "))
	}
}

// anyToString converts a value to a readable string. Handles []interface{} from JSON round-trip.
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []any:
		ss := make([]string, 0, len(val))
		for _, item := range val {
			ss = append(ss, fmt.Sprint(item))
		}
		return strings.Join(ss, ", ")
	default:
		return fmt.Sprint(v)
	}
}

// runSessionBlocking runs a single session synchronously (called from background goroutine).
// Returns the session outcome for the main loop to process.
func (l *Loop) runSessionBlocking(ctx context.Context, alerts []map[string]any) sessionOutcome {
	l.resetDailyCounterIfNeeded()

	if l.cfg.MaxSessionsPerDay > 0 && l.dailySessions >= l.cfg.MaxSessionsPerDay {
		l.log.Warn("Daily session cap reached, skipping",
			zap.Int("cap", l.cfg.MaxSessionsPerDay),
			zap.Int("count", l.dailySessions),
		)
		l.tgMu.Lock()
		l.pendingHumanMsg = nil
		l.tgMu.Unlock()
		l.sessionActive.Store(false)
		return sessionOutcome{}
	}

	l.attemptNum++
	sessionID := fmt.Sprintf("%s-s%d", l.cfg.AgentID, l.attemptNum)

	l.log.Info("Starting session",
		zap.String("session_id", sessionID),
		zap.Int("attempt", l.attemptNum),
		zap.Int("daily", l.dailySessions+1),
	)

	l.lifecycle.Emit(ctx, "session.started", sessionID, nil)

	// Haiku triage: if any pending alerts request a triage pre-filter, run it first.
	if hasTriage(alerts) {
		proceed, triageAnalysis := l.runTriageSession(ctx, sessionID, alerts)
		if !proceed {
			l.log.Info("Triage session decided: skip full session", zap.String("session_id", sessionID))
			l.lifecycle.Emit(ctx, "session.skipped", sessionID, map[string]any{"reason": "triage_no"})
			l.sessionActive.Store(false)
			return sessionOutcome{sessionID: sessionID}
		}
		l.log.Info("Triage session decided: proceed with full session", zap.String("session_id", sessionID))
		l.triageOutput = triageAnalysis
	}

	l.sessionNum++
	l.dailySessions++

	// Persist session number immediately so it survives crashes/restarts.
	// Result details are appended after the session completes.
	l.appendSessionStart(sessionID)

	l.clearRequested.Store(false)

	sessionCtx, sessionCancel := context.WithCancel(ctx)
	defer sessionCancel()

	// Cleanup: nil sessionCancel so /clear and other callers don't try to cancel a dead context.
	// Note: sessionActive is cleared in the wrapper goroutine AFTER sessionDoneCh send.
	defer func() {
		l.sessionCancelMu.Lock()
		l.sessionCancel = nil
		l.sessionCancelMu.Unlock()
		if l.tg != nil {
			l.tg.FlushOutput(ctx)
		}
	}()

	l.sessionCancelMu.Lock()
	l.sessionCancel = sessionCancel
	l.sessionCancelMu.Unlock()

	if l.cfg.SessionTimeout > 0 {
		var timeoutCancel context.CancelFunc
		sessionCtx, timeoutCancel = context.WithTimeout(sessionCtx, l.cfg.SessionTimeout)
		defer timeoutCancel()
	}

	// Read pendingHumanMsg and forceNewSession in one critical section.
	l.tgMu.Lock()
	humanMsg := l.pendingHumanMsg
	l.pendingHumanMsg = nil
	forceNew := l.forceNewSession
	if forceNew {
		l.forceNewSession = false
	}
	l.tgMu.Unlock()

	// Determine session mode.
	var prompt string
	var mode string
	if forceNew {
		mode = "after_clear"
		l.exec.Continue = false
		if humanMsg != nil {
			// User sent a message right after /clear - use it as the prompt
			// for the fresh session instead of the standard trading prompt.
			prompt = fmt.Sprintf("[Telegram from %s at %s]: %s",
				humanMsg.Username, humanMsg.Timestamp.UTC().Format("15:04"), humanMsg.Text)
		} else {
			prompt = l.buildPrompt(sessionID, alerts)
		}
		l.log.Info("Fresh session after /clear", zap.String("session_id", sessionID))
	} else if humanMsg != nil && l.sessionNum > 1 {
		mode = "continue"
		l.exec.Continue = true
		prompt = fmt.Sprintf("[Telegram from %s at %s]: %s",
			humanMsg.Username, humanMsg.Timestamp.UTC().Format("15:04"), humanMsg.Text)
		l.log.Info("Human-initiated session via Telegram (continue)",
			zap.String("session_id", sessionID),
			zap.String("from", humanMsg.Username),
		)
	} else if len(alerts) > 0 && l.sessionNum > 1 {
		// Alert wakeup from Killed state: --continue so Claude has prior context.
		mode = "continue"
		l.exec.Continue = true
		prompt = l.buildPrompt(sessionID, alerts)
		l.log.Info("Alert-initiated session (continue)",
			zap.String("session_id", sessionID),
			zap.Int("alerts", len(alerts)),
		)
	} else {
		mode = "fresh"
		l.exec.Continue = false
		prompt = l.buildPrompt(sessionID, alerts)
		if humanMsg != nil {
			l.tgMu.Lock()
			l.pendingHumanMsg = humanMsg
			l.tgMu.Unlock()
		}
	}

	l.sessionLog.Info(fmt.Sprintf("Session #%d started (%s)", l.sessionNum, mode),
		zap.String("session_id", sessionID),
		zap.String("mode", mode),
		zap.Int("daily", l.dailySessions),
	)

	// Only announce session start for timer-triggered fresh sessions.
	// --continue sessions (telegram, alert) are seamless - no announcement needed.
	if l.tg != nil && mode != "continue" {
		_ = l.tg.SendText(ctx, fmt.Sprintf("-- Session #%d started --", l.sessionNum))
	}

	sessionStart := time.Now().UTC()
	result := l.exec.Run(sessionCtx, prompt)

	// Session summary log.
	l.sessionLog.Info(fmt.Sprintf("Session #%d summary", l.sessionNum),
		zap.String("session_id", sessionID),
		zap.Duration("elapsed", result.Elapsed),
		zap.Int("tool_calls", result.ToolCalls),
		zap.Int("output_chars", result.OutputChars),
	)

	// Cache last snippet for next session's context.
	if result.Output != "" {
		lines := strings.Split(strings.TrimSpace(result.Output), "\n")
		start := len(lines) - 5
		if start < 0 {
			start = 0
		}
		l.lastSessionSnippet = strings.Join(lines[start:], "\n")
	}

	extra := map[string]any{
		"session_num": l.sessionNum,
		"elapsed_ms":  result.Elapsed.Milliseconds(),
	}

	// /clear and recycle kills are not errors - strip the SIGTERM exit error
	// before writing the trail so these don't show up as failures.
	if l.clearRequested.CompareAndSwap(true, false) {
		result.Error = nil
		l.appendSessionTrail(sessionID, sessionStart, result)
		l.log.Info("Session interrupted by /clear", zap.String("session_id", sessionID))
		l.lifecycle.Emit(ctx, "session.cleared", sessionID, extra)
		return sessionOutcome{sessionID: sessionID, startedAt: sessionStart, result: result, cleared: true}
	}
	if l.recycleRequested.Load() {
		result.Error = nil
		l.appendSessionTrail(sessionID, sessionStart, result)
		// Recycle kill from timer - not an error. Don't clear the flag here;
		// handleSessionDone reads it to know it should start a new session immediately.
		l.lifecycle.Emit(ctx, "session.completed", sessionID, extra)
		return sessionOutcome{sessionID: sessionID, startedAt: sessionStart, result: result}
	}

	l.appendSessionTrail(sessionID, sessionStart, result)

	if result.Error != nil {
		// Real error (crash, auth failure, etc.)
		l.consecutiveErrors++
		extra["error"] = result.Error.Error()

		var authErr *AuthError
		var rateLimitErr *RateLimitError
		switch {
		case isError(result.Error, &authErr):
			l.log.Error("Auth error - needs attention", zap.Error(result.Error))
			l.lifecycle.EmitSync(ctx, "needs_attention", sessionID, map[string]any{
				"error_type":    "auth_expired",
				"error_message": result.Error.Error(),
			})
			return sessionOutcome{sessionID: sessionID, startedAt: sessionStart, result: result, fatal: true}
		case isError(result.Error, &rateLimitErr):
			l.log.Warn("Rate limit hit - needs attention", zap.Error(result.Error))
			l.lifecycle.Emit(ctx, "needs_attention", sessionID, map[string]any{
				"error_type":    "rate_limited",
				"error_message": result.Error.Error(),
			})
		default:
			l.log.Error("Session failed", zap.Error(result.Error), zap.Int("consecutive", l.consecutiveErrors))
			l.lifecycle.Emit(ctx, "session.failed", sessionID, extra)
		}

		if l.cfg.MaxConsecutiveErrors > 0 && l.consecutiveErrors >= l.cfg.MaxConsecutiveErrors {
			l.log.Error("Max consecutive errors reached - needs attention",
				zap.Int("limit", l.cfg.MaxConsecutiveErrors),
			)
			l.lifecycle.EmitSync(ctx, "needs_attention", sessionID, map[string]any{
				"error_type":    "max_consecutive_errors",
				"error_message": fmt.Sprintf("%d consecutive failures", l.consecutiveErrors),
			})
			return sessionOutcome{sessionID: sessionID, startedAt: sessionStart, result: result, fatal: true}
		}
	} else {
		// Normal completion (includes recycle/clear kills - already handled above).
		l.consecutiveErrors = 0
		l.lifecycle.Emit(ctx, "session.completed", sessionID, extra)
		l.log.Info("Session completed", zap.Duration("elapsed", result.Elapsed))
	}

	return sessionOutcome{sessionID: sessionID, startedAt: sessionStart, result: result}
}

// hasTriage returns true if any alert has on_trigger=wake_triage.
func hasTriage(alerts []map[string]any) bool {
	for _, a := range alerts {
		if ot, _ := a["on_trigger"].(string); ot == "wake_triage" {
			return true
		}
	}
	return false
}

// runTriageSession runs a lightweight Haiku pre-filter to decide if a full session is needed.
// Returns (proceed, triageOutput) - triageOutput is Haiku's full response for the expensive session.
func (l *Loop) runTriageSession(ctx context.Context, sessionID string, alerts []map[string]any) (bool, string) {
	var sb strings.Builder
	sb.WriteString("You are a trading agent triage assistant. Review the following triggered alerts and decide whether they require immediate action from the full trading agent.\n\n")
	sb.WriteString("## Triggered Alerts\n")
	for _, a := range alerts {
		sb.WriteString(formatSingleAlert(a))
		sb.WriteByte('\n')
		if tp, _ := a["triage_prompt"].(string); tp != "" {
			alertID, _ := a["alert_id"].(string)
			sb.WriteString(fmt.Sprintf("  Triage hint for %s: %s\n", alertID, tp))
		}
	}
	sb.WriteString("\n## Task\n")
	sb.WriteString("Reply with exactly one word on the first line: YES if the full trading agent should run now, NO if it can wait until the next scheduled session.\n")
	sb.WriteString("Then on the next line, one sentence explaining why.\n")
	sb.WriteString("Do not use any tools. Do not output anything else.\n")

	triageCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	result := l.triageExec.Run(triageCtx, sb.String())
	if result.Error != nil {
		l.log.Warn("Triage session error - defaulting to proceed",
			zap.String("session_id", sessionID),
			zap.Error(result.Error),
		)
		return true, "" // fail open
	}

	firstLine := strings.TrimSpace(strings.SplitN(result.Output, "\n", 2)[0])
	decision := strings.ToUpper(firstLine)
	l.log.Info("Triage decision",
		zap.String("session_id", sessionID),
		zap.String("decision", decision),
		zap.String("output", result.Output),
	)
	return !strings.HasPrefix(decision, "NO"), result.Output
}

func (l *Loop) buildPrompt(sessionID string, alerts []map[string]any) string {
	tmpl, err := os.ReadFile(l.cfg.StartupPromptFile)
	if err != nil {
		l.log.Warn("Startup prompt file not found, using fallback",
			zap.String("path", l.cfg.StartupPromptFile), zap.Error(err))
		return fmt.Sprintf("Session %d at %s\n\nRun /trading-session",
			l.sessionNum, time.Now().UTC().Format(time.RFC3339))
	}

	prompt := strings.ReplaceAll(string(tmpl), "\r\n", "\n")
	prompt = strings.ReplaceAll(prompt, "{{UTC_TIME}}", time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
	prompt = strings.ReplaceAll(prompt, "{{SESSION_NUMBER}}", fmt.Sprintf("%d", l.sessionNum))
	prompt = strings.ReplaceAll(prompt, "{{DAILY_SESSION_COUNT}}", fmt.Sprintf("%d", l.dailySessions))
	prompt = strings.ReplaceAll(prompt, "{{AGENT_ID}}", l.cfg.AgentID)

	if l.lastSessionSnippet != "" {
		snippet := fmt.Sprintf("## LAST SESSION SUMMARY\n%s", l.lastSessionSnippet)
		prompt = replaceBlock(prompt, "{{#if LAST_SESSION_SNIPPET}}", "{{/if}}", snippet)
	} else {
		prompt = removeBlock(prompt, "{{#if LAST_SESSION_SNIPPET}}", "{{/if}}")
	}

	if len(alerts) > 0 {
		var sb strings.Builder
		sb.WriteString("## ALERTS TRIGGERED (reason for this session)\n")
		for _, a := range alerts {
			sb.WriteString(formatSingleAlert(a))
			sb.WriteByte('\n')
		}
		if l.triageOutput != "" {
			sb.WriteString("\n### Triage Pre-Analysis (from fast model)\n")
			sb.WriteString(l.triageOutput)
			sb.WriteByte('\n')
			l.triageOutput = ""
		}
		block := sb.String()
		before := prompt
		prompt = replaceBlock(prompt, "{{#if TRIGGERED_ALERTS}}", "{{/if}}", block)
		if prompt == before {
			l.log.Warn("TRIGGERED_ALERTS block not found in template - alerts appended to prompt instead",
				zap.Int("alert_count", len(alerts)))
			prompt += "\n\n" + block
		}
	} else {
		prompt = removeBlock(prompt, "{{#if TRIGGERED_ALERTS}}", "{{/if}}")
	}

	return prompt
}

// pollMCPAlerts calls an MCP endpoint to fetch (and ack) triggered alerts for this agent.
func (l *Loop) pollMCPAlerts(client *mcp.Client, toolName string) []map[string]any {
	result, err := client.CallTool(toolName, map[string]any{})
	if err != nil {
		l.log.Debug("Alert poll failed", zap.String("tool", toolName), zap.Error(err))
		return nil
	}

	if result.IsError {
		if len(result.Content) > 0 {
			l.log.Debug("Alert poll tool error", zap.String("tool", toolName), zap.String("msg", result.Content[0].Text))
		}
		return nil
	}

	if len(result.Content) == 0 || result.Content[0].Text == "" {
		return nil
	}

	var payload struct {
		Alerts []map[string]any `json:"alerts"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		return nil
	}
	return payload.Alerts
}

func (l *Loop) resetDailyCounterIfNeeded() {
	now := time.Now().UTC()
	if now.Year() != l.lastReset.Year() || now.Month() != l.lastReset.Month() || now.Day() != l.lastReset.Day() {
		l.dailySessions = 0
		l.lastReset = now
	}
}

// dedupAlerts removes alerts that are already in pendingAlerts (by alert_id).
func (l *Loop) dedupAlerts(newAlerts []map[string]any) []map[string]any {
	existing := make(map[string]bool, len(l.pendingAlerts))
	for _, a := range l.pendingAlerts {
		if id, _ := a["alert_id"].(string); id != "" {
			existing[id] = true
		}
	}
	var deduped []map[string]any
	for _, a := range newAlerts {
		id, _ := a["alert_id"].(string)
		if id == "" {
			l.log.Warn("Dropping alert with no alert_id from poll response")
			continue
		}
		if !existing[id] {
			deduped = append(deduped, a)
		}
	}
	return deduped
}

func replaceBlock(s, open, close, content string) string {
	start := strings.Index(s, open)
	if start < 0 {
		return s
	}
	end := strings.Index(s[start:], close)
	if end < 0 {
		return s
	}
	end += start + len(close)
	return s[:start] + content + s[end:]
}

// RemoveBlock removes a conditional block from the template.
func RemoveBlock(s, open, close string) string {
	return removeBlock(s, open, close)
}

func removeBlock(s, open, close string) string {
	for {
		start := strings.Index(s, open)
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], close)
		if end < 0 {
			break
		}
		end += start + len(close)
		s = s[:start] + s[end:]
	}
	return s
}

// drainTelegram reads inbound Telegram messages and either injects them into the
// active session or queues them to trigger a new --continue session.
func (l *Loop) drainTelegram(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-l.tg.Inbound():
			if !ok {
				return
			}

			// Intercept /clear command - don't pass to Claude.
			if isClearCommand(msg.Text) {
				l.handleClear(ctx, msg)
				continue
			}

			if l.sessionActive.Load() {
				// Active session: try injecting via stdin.
				if err := l.exec.InjectMessage(msg.Text); err != nil {
					// Inject failed (pipe closed) - queue for immediate --continue.
					l.tgMu.Lock()
					l.pendingHumanMsg = &msg
					l.tgMu.Unlock()
					l.log.Info("Inject failed, queued for next session",
						zap.String("from", msg.Username),
						zap.Error(err),
					)
				} else {
					// Idle -> Working: wake main loop to stop recycle timer.
					l.log.Info("Telegram message injected into active session",
						zap.String("from", msg.Username),
					)
					select {
					case l.tgWake <- struct{}{}:
					default:
					}
				}
			} else {
				// No active session: queue message and wake the loop.
				l.tgMu.Lock()
				l.pendingHumanMsg = &msg
				l.tgMu.Unlock()
				select {
				case l.tgWake <- struct{}{}:
				default:
				}
				l.log.Info("Telegram message queued - waking loop",
					zap.String("from", msg.Username),
					zap.String("text", msg.Text),
				)
			}
		}
	}
}

// handleClear processes the /clear command: kills the active session (if any),
// forces the next session to start fresh (no --continue), and notifies the user.
//
// Runs on drainTelegram goroutine. Must NOT access intervalTimer (owned by main goroutine).
// Instead, signals tgWake so the main loop can stop the timer.
func (l *Loop) handleClear(ctx context.Context, msg TelegramMessage) {
	l.log.Info("Clear command received via Telegram", zap.String("from", msg.Username))

	l.tgMu.Lock()
	l.forceNewSession = true
	l.pendingHumanMsg = nil
	l.tgMu.Unlock()

	// Read sessionCancel atomically - use cancel != nil as the active check
	// instead of sessionActive.Load() to avoid TOCTOU with session transitions.
	l.sessionCancelMu.Lock()
	cancel := l.sessionCancel
	l.sessionCancelMu.Unlock()

	wasActive := cancel != nil

	if wasActive {
		l.clearRequested.Store(true)
		cancel()
	} else {
		// Wake the main loop so it can stop the interval timer and start a new one.
		// Cannot call stopTimer() here - intervalTimer is owned by main goroutine.
		select {
		case l.tgWake <- struct{}{}:
		default:
		}
	}

	nextAt := time.Now().Add(l.cfg.Interval).UTC().Format("15:04")
	reply := fmt.Sprintf("Cleared. Next fresh session at %s UTC.", nextAt)
	if err := l.tg.SendText(ctx, reply); err != nil {
		l.log.Warn("Failed to send /clear reply", zap.Error(err))
	}
}

// isClearCommand checks if the message is a /clear or /reset command.
func isClearCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	cmd := strings.Fields(text)[0]
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	return cmd == "/clear" || cmd == "/reset"
}

// isError checks if err matches the target type via errors.As.
func isError[T error](err error, target *T) bool {
	if err == nil {
		return false
	}
	var t T
	ok := false
	e := err
	for e != nil {
		if te, ok2 := e.(T); ok2 {
			*target = te
			ok = ok2
			break
		}
		e = unwrap(e)
	}
	_ = t
	return ok
}

type unwrapper interface{ Unwrap() error }

func unwrap(err error) error {
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}

// seedSessionCounters reads the session trail JSONL file to restore sessionNum
// and dailySessions across process restarts. Only counts entries for this agent.
func (l *Loop) seedSessionCounters() {
	if l.cfg.SessionTrailFile == "" {
		return
	}

	f, err := os.Open(l.cfg.SessionTrailFile)
	if err != nil {
		return // file doesn't exist yet - first run
	}
	defer func() { _ = f.Close() }()

	todayPrefix := time.Now().UTC().Format("2006-01-02")
	agentID := l.cfg.AgentID

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry struct {
			AgentID    string `json:"agent_id"`
			SessionNum int    `json:"session_num"`
			StartedAt  string `json:"started_at"`
			Event      string `json:"event"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.AgentID != agentID {
			continue
		}
		if entry.SessionNum > l.sessionNum {
			l.sessionNum = entry.SessionNum
		} else if entry.SessionNum == 0 {
			// Fallback for trail entries written before session_num was added.
			l.sessionNum++
		}
		// entry.SessionNum <= l.sessionNum (and non-zero): duplicate or out-of-order, skip.
		//
		// Only count "started" entries for daily sessions. Each session writes two entries
		// (started + completed), so counting all would double the daily count.
		// Blank event = legacy single-entry format, count it too.
		if strings.HasPrefix(entry.StartedAt, todayPrefix) && (entry.Event == "" || entry.Event == "started") {
			l.dailySessions++
		}
	}

	if l.sessionNum > 0 {
		l.log.Info("Restored session counters from trail",
			zap.Int("session_num", l.sessionNum),
			zap.Int("daily_sessions", l.dailySessions),
		)
	}
}

// appendSessionStart persists the session number immediately so it survives crashes.
// seedSessionCounters reads this on restart to avoid repeating the same number.
func (l *Loop) appendSessionStart(sessionID string) {
	l.writeTrailEntry(map[string]any{
		"session_id":  sessionID,
		"agent_id":    l.cfg.AgentID,
		"session_num": l.sessionNum,
		"started_at":  time.Now().UTC().Format(time.RFC3339),
		"event":       "started",
	})
}

// appendSessionTrail appends session result to the JSONL trail file.
func (l *Loop) appendSessionTrail(sessionID string, startedAt time.Time, result SessionResult) {
	snippet := result.Output
	if len(snippet) > 200 {
		snippet = snippet[len(snippet)-200:]
	}

	entry := map[string]any{
		"session_id":     sessionID,
		"agent_id":       l.cfg.AgentID,
		"session_num":    l.sessionNum,
		"started_at":     startedAt.UTC().Format(time.RFC3339),
		"elapsed_ms":     result.Elapsed.Milliseconds(),
		"output_snippet": snippet,
		"event":          "completed",
	}
	if result.Error != nil {
		entry["error"] = result.Error.Error()
	}
	l.writeTrailEntry(entry)
}

// writeTrailEntry appends one JSON line to the JSONL session trail file.
func (l *Loop) writeTrailEntry(entry map[string]any) {
	if l.cfg.SessionTrailFile == "" {
		return
	}

	line, err := json.Marshal(entry)
	if err != nil {
		l.log.Warn("Session trail marshal failed", zap.Error(err))
		return
	}

	dir := filepath.Dir(l.cfg.SessionTrailFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		l.log.Warn("Session trail mkdir failed", zap.String("dir", dir), zap.Error(err))
		return
	}

	f, err := os.OpenFile(l.cfg.SessionTrailFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		l.log.Warn("Session trail open failed", zap.String("path", l.cfg.SessionTrailFile), zap.Error(err))
		return
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(line, '\n')); err != nil {
		l.log.Warn("Session trail write failed", zap.String("path", l.cfg.SessionTrailFile), zap.Error(err))
	}
}
