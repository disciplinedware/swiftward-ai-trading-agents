package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"ai-trading-agents/internal/agents/random"
	"ai-trading-agents/internal/agents/simple_llm"
	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/dashboard"
	"ai-trading-agents/internal/observability"
	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/exchange"
	"ai-trading-agents/internal/marketdata"
	"ai-trading-agents/internal/marketdata/binance"
	"ai-trading-agents/internal/marketdata/bybit"
	"ai-trading-agents/internal/marketdata/kraken"
	"ai-trading-agents/internal/marketdata/prism"
	"ai-trading-agents/internal/marketdata/simulated"
	"ai-trading-agents/internal/newsdata"
	"ai-trading-agents/internal/newsdata/cryptocompare"
	"ai-trading-agents/internal/newsdata/cryptopanic"
	mktdata "ai-trading-agents/internal/mcps/marketdata"
	"ai-trading-agents/internal/mcps/news"
	"ai-trading-agents/internal/mcps/polymarket"
	"ai-trading-agents/internal/mcps/codesandbox"
	"ai-trading-agents/internal/mcps/files"
	"ai-trading-agents/internal/mcps/risk"
	"ai-trading-agents/internal/mcps/trading"
	"ai-trading-agents/internal/platform"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	// Initialize logger with optional OTLP bridge (logs → SigNoz via OTEL Collector).
	log, err := observability.NewLogger(cfg.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		return 1
	}
	defer func() { _ = log.Sync() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		observability.ShutdownLogProvider(ctx)
	}()

	// Parse roles
	roles, err := config.ReadRoles(cfg)
	if err != nil {
		log.Error("Failed to read roles", zap.Error(err))
		return 1
	}
	log.Info("Starting AI Trading Agents Platform", zap.Strings("roles", roles))

	// Signal context
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	// Shared HTTP router
	router := chi.NewRouter()
	router.Use(chimiddleware.Recoverer)
	router.Use(chimiddleware.RealIP)

	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"ai-trading-agents"}`))
	})

	// Service context
	svcCtx := platform.NewServiceContext(ctx, log, cfg, router, roles)

	// Shared infra
	commissionRate, _ := decimal.NewFromString(cfg.Exchange.CommissionRate)
	log.Info("Exchange commission rate", zap.String("rate", commissionRate.String()))

	var exchClient exchange.Client
	switch cfg.Exchange.Mode {
	case "paper":
		registry := marketdata.NewSymbolRegistry()
		paperSrc := binance.NewSource(registry, binance.Config{}, log.Named("market_data_binance"))
		exchClient = exchange.NewPaperClient(paperSrc, log.Named("exchange"), commissionRate)
	case "kraken_paper":
		exchClient = exchange.NewKrakenClient(log.Named("exchange"), exchange.KrakenConfig{
			Bin:      cfg.Exchange.KrakenBin,
			Sandbox:  true,
			StateDir: cfg.Exchange.KrakenStateDir,
		})
	case "kraken_real":
		kc := exchange.NewKrakenClient(log.Named("exchange"), exchange.KrakenConfig{
			Bin:      cfg.Exchange.KrakenBin,
			Sandbox:  false,
			StateDir: cfg.Exchange.KrakenStateDir,
			Pairs:    cfg.MarketData.Markets,
			// API credentials: CLI reads KRAKEN_API_KEY + KRAKEN_API_SECRET from env vars.
		})
		balances, balErr := kc.GetBalance()
		if balErr != nil {
			log.Fatal("kraken_real: credential check failed - fix KRAKEN_API_KEY/KRAKEN_API_SECRET and restart", zap.Error(balErr))
		}
		log.Info("Kraken credentials verified", zap.Int("assets", len(balances)))
		exchClient = kc
	default:
		exchClient, err = exchange.NewClient(cfg.Exchange.Mode, cfg.Exchange.URL, log.Named("exchange"), commissionRate)
		if err != nil {
			log.Error("Failed to create exchange client", zap.Error(err))
			return 1
		}
	}

	// Database (required for trading_mcp, risk_mcp, and market_data_mcp roles)
	var repo db.Repository
	needsDB := contains(roles, config.RoleTradingMCP) || contains(roles, config.RoleRiskMCP) || contains(roles, config.RoleMarketDataMCP) || contains(roles, config.RoleNewsMCP)
	if needsDB {
		pool, poolErr := db.NewPool(ctx, cfg.DB.URL, log)
		if poolErr != nil {
			log.Error("Failed to connect to trading database", zap.Error(poolErr))
			return 1
		}
		defer pool.Close()

		repo = db.NewPgxRepository(pool)
	}

	// Create services based on roles
	var svcs []platform.IService

	// Trading MCP must be created first (Risk MCP depends on it)
	var tradingSvc *trading.Service
	if contains(roles, config.RoleTradingMCP) {
		tradingSvc = trading.NewService(svcCtx, exchClient, repo)
		svcs = append(svcs, tradingSvc)
	}

	if contains(roles, config.RoleRiskMCP) {
		if tradingSvc == nil {
			log.Error("risk_mcp requires trading_mcp role to be active")
			return 1
		}
		riskSvc := risk.NewService(svcCtx, tradingSvc)
		svcs = append(svcs, riskSvc)
	}

	if contains(roles, config.RoleFilesMCP) {
		filesSvc := files.NewService(svcCtx)
		svcs = append(svcs, filesSvc)
	}

	if contains(roles, config.RoleCodeMCP) {
		codeSvc := codesandbox.NewService(svcCtx)
		svcs = append(svcs, codeSvc)
	}

	if contains(roles, config.RoleMarketDataMCP) {
		registry := marketdata.NewSymbolRegistry()
		source, err := buildMarketDataSource(cfg.MarketData, registry, exchClient, log)
		if err != nil {
			log.Error("Failed to build market data source", zap.Error(err))
			return 1
		}
		log.Info("Market data source ready", zap.String("source", source.Name()))
		marketDataSvc := mktdata.NewService(svcCtx, source, repo)

		// Attach PRISM client for market intelligence tools (fear/greed, technicals, signals).
		if cfg.MarketData.Prism.Enabled && cfg.MarketData.Prism.APIKey != "" {
			timeout, _ := time.ParseDuration(cfg.MarketData.Prism.Timeout)
			if timeout == 0 {
				timeout = 10 * time.Second
			}
			cooldown, _ := time.ParseDuration(cfg.MarketData.Prism.Cooldown)
			if cooldown == 0 {
				cooldown = 60 * time.Second
			}
			prismClient := prism.NewClient(prism.Config{
				BaseURL:          cfg.MarketData.Prism.BaseURL,
				APIKey:           cfg.MarketData.Prism.APIKey,
				Timeout:          timeout,
				FailureThreshold: cfg.MarketData.Prism.FailureThreshold,
				Cooldown:         cooldown,
			}, log)
			marketDataSvc.SetPrism(prismClient)
			log.Info("PRISM market intelligence enabled",
				zap.String("base_url", cfg.MarketData.Prism.BaseURL))
		}

		svcs = append(svcs, marketDataSvc)

		// Wire market data source into trading service for position alert polling.
		if tradingSvc != nil {
			tradingSvc.SetMarketSource(source)
			log.Info("Market data source wired into trading service for position alerts")
		}
	}

	if contains(roles, config.RoleNewsMCP) {
		newsSource, err := buildNewsSource(cfg.News, log)
		if err != nil {
			log.Error("Failed to build news source", zap.Error(err))
			return 1
		}
		log.Info("News source ready", zap.String("source", newsSource.Name()))
		newsSvc := news.NewService(svcCtx, newsSource, repo)
		svcs = append(svcs, newsSvc)
	}

	if contains(roles, config.RolePolymarketMCP) {
		polymarketSvc := polymarket.NewService(svcCtx)
		svcs = append(svcs, polymarketSvc)
	}

	if contains(roles, config.RoleRandomAgent) {
		agentSvc := random.NewService(svcCtx)
		svcs = append(svcs, agentSvc)
	}

	if contains(roles, config.RoleLLMAgent) {
		llmSvc := simple_llm.NewService(svcCtx)
		svcs = append(svcs, llmSvc)
	}

	// Dashboard must be registered last so its catch-all route doesn't shadow specific routes.
	if contains(roles, config.RoleDashboard) {
		dashSvc := dashboard.NewService(svcCtx)
		svcs = append(svcs, dashSvc)
	}

	if len(svcs) == 0 {
		log.Error("No services created — check roles")
		return 1
	}

	// Initialize all services
	for _, svc := range svcs {
		if err := svc.Initialize(); err != nil {
			log.Error("Service initialization error", zap.Error(err))
			return 1
		}
	}

	// Start HTTP server
	httpServer := &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: router,
	}
	g.Go(func() error {
		log.Info("HTTP server starting", zap.String("addr", cfg.Server.Addr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	// Shut down HTTP server when context is cancelled (e.g., by signal or service exit).
	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	})

	// Start all services
	for _, svc := range svcs {
		s := svc
		g.Go(func() error {
			return s.Start()
		})
	}

	// Wait for context cancellation or service error.
	// ErrSessionDone is expected from LLM agent in once mode - not an error.
	waitErr := g.Wait()
	if waitErr != nil && !errors.Is(waitErr, simple_llm.ErrSessionDone) {
		log.Error("Service stopped with error", zap.Error(waitErr))
	}
	log.Info("Shutting down...")

	// Graceful shutdown of services (HTTP server already shut down by the errgroup goroutine above).
	for _, svc := range svcs {
		if err := svc.Stop(); err != nil {
			log.Error("Service stop error", zap.Error(err))
		}
	}

	log.Info("Shutdown complete")
	if waitErr != nil && !errors.Is(waitErr, simple_llm.ErrSessionDone) {
		return 1
	}
	return 0
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// buildMarketDataSource constructs a DataSource from the ordered sources list.
// "simulated" is only valid as a standalone source (local testing only).
// Multiple real sources are wrapped in a ChainSource that degrades in order on failure.
func buildMarketDataSource(cfg config.MarketDataConfig, registry *marketdata.SymbolRegistry, exchClient exchange.Client, log *zap.Logger) (marketdata.DataSource, error) {
	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("market_data.sources is empty - specify at least one source (binance, simulated)")
	}

	if len(cfg.Sources) > 1 {
		for _, name := range cfg.Sources {
			if name == "simulated" {
				return nil, fmt.Errorf("simulated source cannot be mixed with real sources - use it standalone for local testing only")
			}
		}
	}

	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("parse market_data.timeout %q: %w", cfg.Timeout, err)
	}

	sources := make([]marketdata.DataSource, 0, len(cfg.Sources))
	for _, name := range cfg.Sources {
		switch name {
		case "kraken":
			src := kraken.NewSource(registry, kraken.Config{Timeout: timeout, MaxAttempts: cfg.MaxAttempts}, log.Named("market_data_kraken"))
			sources = append(sources, src)
		case "binance":
			src := binance.NewSource(registry, binance.Config{Timeout: timeout}, log.Named("market_data_binance"))
			sources = append(sources, src)
		case "bybit":
			src := bybit.NewSource(bybit.Config{Timeout: timeout}, log.Named("market_data_bybit"))
			sources = append(sources, src)
		case "simulated":
			src := simulated.NewSource(exchClient, cfg.Markets, cfg.Volatility, cfg.CandleHistory, log.Named("market_data_simulated"))
			sources = append(sources, src)
		default:
			return nil, fmt.Errorf("unknown market data source %q (valid: kraken, binance, bybit, simulated)", name)
		}
	}

	if len(sources) == 1 {
		return sources[0], nil
	}
	return marketdata.NewChainSource(sources, log.Named("market_data_chain")), nil
}

// buildNewsSource constructs a news Source from config.
func buildNewsSource(cfg config.NewsConfig, log *zap.Logger) (newsdata.Source, error) {
	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("news.sources is empty - specify at least one source (cryptocompare, cryptopanic)")
	}

	sourceName := cfg.Sources[0]
	switch sourceName {
	case "cryptocompare":
		return cryptocompare.NewSource(cryptocompare.Config{}), nil
	case "cryptopanic":
		if cfg.CryptoPanicToken == "" {
			log.Warn("news.cryptopanic_token is empty - CryptoPanic API calls will fail. Set TRADING__NEWS__CRYPTOPANIC_TOKEN.")
		}
		return cryptopanic.NewSource(cryptopanic.Config{
			AuthToken: cfg.CryptoPanicToken,
		}), nil
	default:
		return nil, fmt.Errorf("unknown news source %q (valid: cryptocompare, cryptopanic)", sourceName)
	}
}
