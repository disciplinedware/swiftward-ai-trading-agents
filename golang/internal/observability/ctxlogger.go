package observability

import (
	"context"

	"go.uber.org/zap"
)

type ctxLoggerKey struct{}

// WithLogger stores a logger in the context.
func WithLogger(ctx context.Context, log *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxLoggerKey{}, log)
}

// LoggerFromCtx retrieves the logger from context, falling back to the provided logger.
func LoggerFromCtx(ctx context.Context, fallback *zap.Logger) *zap.Logger {
	if l, ok := ctx.Value(ctxLoggerKey{}).(*zap.Logger); ok && l != nil {
		return l
	}
	return fallback
}
