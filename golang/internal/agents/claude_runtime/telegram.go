package claude_runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	tu "github.com/mymmrac/telego/telegoutil"
	"go.uber.org/zap"
)

// TelegramMessage is an inbound message from a Telegram user.
type TelegramMessage struct {
	Text      string
	UserID    int64
	Username  string
	Timestamp time.Time
}

// TelegramBot owns the Telegram bot connection: polling, sending, typing, output buffering.
// Uses the placeholder+edit pattern (ported from PicoClaw): sends a placeholder message,
// then edits it in place as Claude's output grows. Each "thought block" (text between tool
// calls) becomes one self-updating Telegram message.
type TelegramBot struct {
	bot       *telego.Bot
	chatID    int64
	topicID   int
	allowUIDs map[int64]bool
	log       *zap.Logger

	inbound chan TelegramMessage

	// Placeholder+edit state (protected by mu).
	mu           sync.Mutex
	currentMsgID int             // active placeholder message ID (0 = none)
	currentBuf   strings.Builder // accumulated content for current message
	lastAppend   time.Time       // when last text was appended
	lastEdit     time.Time       // when last edit was sent
	lastTyping   time.Time

	cancel context.CancelFunc
}

// NewTelegramBot creates a bot but does not start polling. Call Start to begin.
func NewTelegramBot(cfg Config, log *zap.Logger) (*TelegramBot, error) {
	// Use net/http caller instead of default fasthttp — net/http respects
	// HTTP_PROXY/HTTPS_PROXY env vars, which is required on the agent-isolated
	// network where DNS can only resolve via the inet-gateway proxy.
	bot, err := telego.NewBot(cfg.TelegramBotToken,
		telego.WithAPICaller(ta.DefaultHTTPCaller),
	)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	allow := make(map[int64]bool, len(cfg.TelegramAllowUIDs))
	for _, uid := range cfg.TelegramAllowUIDs {
		allow[uid] = true
	}

	return &TelegramBot{
		bot:       bot,
		chatID:    cfg.TelegramChatID,
		topicID:   cfg.TelegramTopicID,
		allowUIDs: allow,
		log:       log,
		inbound:   make(chan TelegramMessage, 32),
	}, nil
}

// Inbound returns the channel of incoming Telegram messages (after allowlist filtering).
func (t *TelegramBot) Inbound() <-chan TelegramMessage {
	return t.inbound
}

// Start begins long-polling for updates and a background output flush goroutine.
func (t *TelegramBot) Start(ctx context.Context) {
	ctx, t.cancel = context.WithCancel(ctx)

	updates, err := t.bot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{
		Timeout:        30,
		AllowedUpdates: []string{"message"},
	})
	if err != nil {
		t.log.Error("Failed to start Telegram polling", zap.Error(err))
		return
	}

	// Message handler goroutine.
	go func() {
		t.log.Info("Telegram polling goroutine started")
		for {
			select {
			case <-ctx.Done():
				t.log.Info("Telegram polling goroutine stopped (context done)")
				return
			case update, ok := <-updates:
				if !ok {
					if ctx.Err() == nil {
						t.log.Error("Telegram updates channel closed unexpectedly - no more inbound messages")
					}
					return
				}
				t.log.Debug("Telegram raw update received", zap.Int("update_id", update.UpdateID))
				t.handleUpdate(update)
			}
		}
	}()

	// Output flush goroutine: checks every 500ms.
	// - If content is accumulating and 2s since last edit: edit the placeholder.
	// - If silence > 1s: finalize current message (start fresh on next text).
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.tryFlush(ctx)
			}
		}
	}()

	t.log.Info("Telegram bot started",
		zap.Int64("chat_id", t.chatID),
		zap.Int("topic_id", t.topicID),
	)
}

// Stop gracefully stops polling.
func (t *TelegramBot) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
}

func (t *TelegramBot) handleUpdate(update telego.Update) {
	msg := update.Message
	if msg == nil {
		t.log.Debug("Telegram update with no message", zap.Int("update_id", update.UpdateID))
		return
	}
	if msg.From == nil {
		return
	}

	// Allowlist check first.
	if len(t.allowUIDs) > 0 && !t.allowUIDs[msg.From.ID] {
		t.log.Debug("Telegram message filtered: user not in allowlist",
			zap.Int64("user_id", msg.From.ID),
			zap.String("username", msg.From.Username),
		)
		return
	}

	// Only accept messages from the configured chat+topic.
	if msg.Chat.ID != t.chatID || (t.topicID != 0 && msg.MessageThreadID != t.topicID) {
		t.log.Debug("Telegram message filtered: wrong chat/topic",
			zap.Int64("chat_id", msg.Chat.ID),
			zap.Int64("expected_chat_id", t.chatID),
			zap.Int("topic_id", msg.MessageThreadID),
			zap.Int("expected_topic_id", t.topicID),
			zap.String("text", msg.Text),
		)
		return
	}

	// In forum groups, text might arrive in different fields.
	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	if text == "" {
		return
	}

	t.log.Info("Telegram message received",
		zap.Int64("user_id", msg.From.ID),
		zap.String("username", msg.From.Username),
		zap.String("text", text),
	)

	username := msg.From.Username
	if username == "" {
		username = msg.From.FirstName
	}

	select {
	case t.inbound <- TelegramMessage{
		Text:      text,
		UserID:    msg.From.ID,
		Username:  username,
		Timestamp: time.Unix(int64(msg.Date), 0),
	}:
	default:
		t.log.Warn("Telegram inbound channel full, dropping message")
	}
}

// SendText sends a new text message to the configured chat/topic.
// Splits markdown first (tag-safe), then converts each chunk to HTML independently.
// Falls back to plain text per-chunk on HTML parse failure.
func (t *TelegramBot) SendText(ctx context.Context, text string) error {
	// Split markdown before conversion to avoid splitting mid-HTML-tag.
	// Use 3500 runes to leave headroom for HTML tag expansion (<b>, <pre><code>, &amp;, etc.).
	mdChunks := splitMessage(text, 3500)

	for _, md := range mdChunks {
		html := markdownToTelegramHTML(md)
		params := &telego.SendMessageParams{
			ChatID:          tu.ID(t.chatID),
			Text:            html,
			ParseMode:       telego.ModeHTML,
			MessageThreadID: t.topicID,
		}
		if _, err := t.bot.SendMessage(ctx, params); err != nil {
			t.log.Debug("Telegram HTML send failed, falling back to plain text", zap.Error(err))
			params.Text = md
			params.ParseMode = ""
			if _, err2 := t.bot.SendMessage(ctx, params); err2 != nil {
				return fmt.Errorf("telegram send: %w", err2)
			}
		}
	}
	return nil
}

// SendTextReturningID sends a plain text message and returns the Telegram message ID.
// Returns 0 if the send fails.
func (t *TelegramBot) SendTextReturningID(ctx context.Context, text string) int {
	params := &telego.SendMessageParams{
		ChatID:          tu.ID(t.chatID),
		Text:            text,
		MessageThreadID: t.topicID,
	}
	sent, err := t.bot.SendMessage(ctx, params)
	if err != nil {
		t.log.Warn("Telegram send failed", zap.Error(err))
		return 0
	}
	return sent.MessageID
}

// DeleteMessage deletes a Telegram message by ID. Silently ignores errors.
func (t *TelegramBot) DeleteMessage(ctx context.Context, msgID int) {
	if msgID == 0 {
		return
	}
	params := &telego.DeleteMessageParams{
		ChatID:    tu.ID(t.chatID),
		MessageID: msgID,
	}
	if err := t.bot.DeleteMessage(ctx, params); err != nil {
		t.log.Debug("Telegram delete failed (message may already be gone)", zap.Int("msg_id", msgID), zap.Error(err))
	}
}

// SendTyping sends a "typing" chat action, throttled to at most once per 5 seconds.
func (t *TelegramBot) SendTyping(ctx context.Context) {
	t.mu.Lock()
	if time.Since(t.lastTyping) < 5*time.Second {
		t.mu.Unlock()
		return
	}
	t.lastTyping = time.Now()
	t.mu.Unlock()

	params := &telego.SendChatActionParams{
		ChatID:          tu.ID(t.chatID),
		Action:          telego.ChatActionTyping,
		MessageThreadID: t.topicID,
	}
	if err := t.bot.SendChatAction(ctx, params); err != nil {
		t.log.Debug("Telegram typing failed", zap.Error(err))
	}
}

// AppendOutput buffers text from Claude's output stream.
// If no placeholder message exists, sends one first.
func (t *TelegramBot) AppendOutput(text string) {
	t.mu.Lock()
	t.currentBuf.WriteString(text)
	t.lastAppend = time.Now()
	t.mu.Unlock()
}

// FlushOutput finalizes any active message and sends remaining content.
// Called at session end.
func (t *TelegramBot) FlushOutput(ctx context.Context) {
	t.mu.Lock()
	content := t.currentBuf.String()
	msgID := t.currentMsgID
	t.currentBuf.Reset()
	t.currentMsgID = 0
	t.mu.Unlock()

	if content == "" {
		return
	}

	if msgID != 0 {
		// Final edit of the active placeholder.
		t.editMessage(ctx, msgID, content)
	} else {
		// No placeholder - send as new message.
		if err := t.SendText(ctx, content); err != nil {
			t.log.Warn("Telegram flush failed", zap.Error(err))
		}
	}
}

// tryFlush is called every 500ms by the flush goroutine.
// Edits the active placeholder with accumulated content, or finalizes on silence gap.
func (t *TelegramBot) tryFlush(ctx context.Context) {
	t.mu.Lock()
	content := t.currentBuf.String()
	if content == "" {
		t.mu.Unlock()
		return
	}

	silenceGap := time.Since(t.lastAppend)
	timeSinceEdit := time.Since(t.lastEdit)
	msgID := t.currentMsgID

	// Silence > 1s: finalize current message and reset for next thought block.
	if silenceGap > 1*time.Second && msgID != 0 {
		t.currentBuf.Reset()
		t.currentMsgID = 0
		t.mu.Unlock()
		t.editMessage(ctx, msgID, content)
		return
	}

	// No placeholder yet: send one.
	if msgID == 0 {
		t.mu.Unlock()
		newID := t.sendPlaceholder(ctx)
		if newID == 0 {
			return
		}
		t.mu.Lock()
		if t.currentMsgID == 0 {
			// We won the race - use our placeholder.
			t.currentMsgID = newID
			t.lastEdit = time.Now()
		}
		// If currentMsgID != 0, another tick beat us - orphaned placeholder is
		// harmless (shows "Analyzing..." briefly, then next edit goes to the winner).
		t.mu.Unlock()
		return
	}

	// Throttle edits to max once per 2s.
	if timeSinceEdit < 2*time.Second {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// Edit placeholder with current content.
	t.editMessage(ctx, msgID, content)
	t.mu.Lock()
	t.lastEdit = time.Now()
	t.mu.Unlock()
}

// sendPlaceholder sends an initial "Analyzing..." message and returns its message ID.
func (t *TelegramBot) sendPlaceholder(ctx context.Context) int {
	params := &telego.SendMessageParams{
		ChatID:          tu.ID(t.chatID),
		Text:            "Thinking...",
		MessageThreadID: t.topicID,
	}
	sent, err := t.bot.SendMessage(ctx, params)
	if err != nil {
		t.log.Warn("Telegram placeholder send failed", zap.Error(err))
		return 0
	}
	return sent.MessageID
}

// editMessage edits an existing message with new content.
// Splits markdown first (code-block-aware), edits the placeholder with the first chunk,
// and sends any remaining chunks as new messages. Falls back to plain text on HTML parse failure.
func (t *TelegramBot) editMessage(ctx context.Context, msgID int, content string) {
	// Split markdown before HTML conversion to avoid splitting mid-tag.
	// 3500 runes leaves headroom for HTML tag expansion (<b>, <pre><code>, &amp;, etc.).
	mdChunks := splitMessage(content, 3500)
	if len(mdChunks) == 0 {
		return
	}

	// Edit the placeholder with the first chunk.
	t.editSingleMessage(ctx, msgID, mdChunks[0])

	// Send remaining chunks as new messages.
	for _, chunk := range mdChunks[1:] {
		if err := t.SendText(ctx, chunk); err != nil {
			t.log.Warn("Telegram overflow chunk send failed", zap.Error(err))
		}
	}
}

// editSingleMessage edits one message with a single chunk (must already fit within limits).
func (t *TelegramBot) editSingleMessage(ctx context.Context, msgID int, markdown string) {
	html := markdownToTelegramHTML(markdown)

	params := &telego.EditMessageTextParams{
		ChatID:    tu.ID(t.chatID),
		MessageID: msgID,
		Text:      html,
		ParseMode: telego.ModeHTML,
	}
	if _, err := t.bot.EditMessageText(ctx, params); err != nil {
		t.log.Debug("Telegram HTML edit failed, falling back to plain text", zap.Int("msg_id", msgID), zap.Error(err))
		params.Text = markdown
		params.ParseMode = ""
		if _, err2 := t.bot.EditMessageText(ctx, params); err2 != nil {
			t.log.Debug("Telegram edit failed", zap.Int("msg_id", msgID), zap.Error(err2))
		}
	}
}
