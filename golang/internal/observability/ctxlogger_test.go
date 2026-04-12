package observability

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestLoggerRoundTrip(t *testing.T) {
	log := zap.NewNop()
	ctx := WithLogger(context.Background(), log)
	got := LoggerFromCtx(ctx, nil)
	if got != log {
		t.Fatal("expected stored logger, got different instance")
	}
}

func TestLoggerFallback(t *testing.T) {
	fallback := zap.NewNop()
	got := LoggerFromCtx(context.Background(), fallback)
	if got != fallback {
		t.Fatal("expected fallback logger")
	}
}

func TestLoggerNilFallback(t *testing.T) {
	got := LoggerFromCtx(context.Background(), nil)
	if got != nil {
		t.Fatal("expected nil when no logger and nil fallback")
	}
}
