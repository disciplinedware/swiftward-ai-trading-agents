package observability

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	globalLogProvider *sdklog.LoggerProvider
	logProviderOnce   sync.Once
)

// NewLogger creates a zap logger. If OTEL_LOGS_EXPORTER=otlp and
// OTEL_EXPORTER_OTLP_LOGS_ENDPOINT are set, logs are tee'd via OTLP
// (to SigNoz OTEL Collector) in addition to stdout.
func NewLogger(format string) (*zap.Logger, error) {
	var zapCfg zap.Config
	switch format {
	case "json":
		zapCfg = zap.NewProductionConfig()
	default:
		zapCfg = zap.Config{
			Level:            zap.NewAtomicLevelAt(zapcore.InfoLevel),
			Development:      false,
			Encoding:         "console",
			EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
		}
	}

	l, err := zapCfg.Build(zap.AddStacktrace(zapcore.DPanicLevel))
	if err != nil {
		return nil, err
	}

	// Attach OTLP log bridge if configured — mirrors swiftward-core logger.go pattern.
	otlpLogsExporter := os.Getenv("OTEL_LOGS_EXPORTER")
	otlpLogsEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
	if otlpLogsExporter == "otlp" && otlpLogsEndpoint != "" {
		serviceName := os.Getenv("OTEL_SERVICE_NAME")
		if serviceName == "" {
			serviceName = "trading-server"
		}
		provider, providerErr := initOTLPLogProvider(otlpLogsEndpoint)
		if providerErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to init OTLP log provider: %v\n", providerErr)
		} else {
			logProviderOnce.Do(func() { globalLogProvider = provider })
			otelCore := otelzap.NewCore(serviceName, otelzap.WithLoggerProvider(provider))
			l = zap.New(zapcore.NewTee(l.Core(), otelCore), zap.AddStacktrace(zapcore.DPanicLevel))
		}
	}

	return l, nil
}

// ShutdownLogProvider flushes and shuts down the OTLP log provider on graceful shutdown.
func ShutdownLogProvider(ctx context.Context) {
	if globalLogProvider != nil {
		_ = globalLogProvider.Shutdown(ctx)
	}
}

func initOTLPLogProvider(rawEndpoint string) (*sdklog.LoggerProvider, error) {
	// Parse URL: host goes to WithEndpoint, path goes to WithURLPath.
	// If no path in URL, default to /v1/logs (OTEL standard).
	// For SigNoz use: http://signoz-otel-collector:4318/v1/logs
	parsed, err := url.Parse(rawEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid OTEL_EXPORTER_OTLP_LOGS_ENDPOINT %q: %w", rawEndpoint, err)
	}
	host := parsed.Host
	path := parsed.Path
	if path == "" {
		path = "/v1/logs"
	}

	exporter, err := otlploghttp.New(
		context.Background(),
		otlploghttp.WithEndpoint(host),
		otlploghttp.WithInsecure(),
		otlploghttp.WithURLPath(path),
	)
	if err != nil {
		return nil, err
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter,
			sdklog.WithExportInterval(500*time.Millisecond),
		)),
	)
	return provider, nil
}
