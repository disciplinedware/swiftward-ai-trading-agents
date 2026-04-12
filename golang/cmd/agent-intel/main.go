// agent-intel downloads, analyzes, and generates a static site for hackathon trading agents.
//
// Usage:
//
//	go run ./cmd/agent-intel                  # full pipeline: sync + calculate + generate
//	go run ./cmd/agent-intel sync             # all sync phases (events + state + market)
//	go run ./cmd/agent-intel sync-events      # events only (fastest, for re-analysis)
//	go run ./cmd/agent-intel sync-state       # state snapshot only (batched view calls)
//	go run ./cmd/agent-intel sync-market      # market data only
//	go run ./cmd/agent-intel calculate        # run FIFO PnL calculation
//	go run ./cmd/agent-intel generate         # build static HTML site
//
// Env var overrides:
//
//	CHAIN_RPC_URL        - Sepolia RPC URL (required for any sync)
//	AGENT_INTEL_STATE_MAX_AGE  - snapshot throttle window (default 5m, e.g. "30s", "1h")
//	AGENT_INTEL_FORCE_STATE    - "1" to bypass snapshot throttle once
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"ai-trading-agents/internal/agentintel"

	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	log, _ := zap.NewDevelopment()
	defer func() { _ = log.Sync() }()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Recover fatal panics so defers (e.g., sync lock release) run before exit.
	defer func() {
		if r := recover(); r != nil {
			if fe, ok := r.(fatalErr); ok {
				log.Error(fe.msg, zap.Error(fe.err))
				os.Exit(1)
			}
			panic(r)
		}
	}()

	// Resolve project root (assume running from golang/ or project root).
	baseDir := "."
	if _, err := os.Stat("golang"); err == nil {
		baseDir = "."
	} else if _, err := os.Stat("../data"); err == nil {
		baseDir = ".."
	}

	paths := agentintel.NewPaths(baseDir)
	if err := paths.EnsureDirs(); err != nil {
		fatal(log, "create dirs", err)
	}

	switch cmd {
	case "sync":
		runSync(ctx, paths, log, agentintel.PhaseAll)
	case "sync-events":
		runSync(ctx, paths, log, agentintel.PhaseEvents)
	case "sync-state":
		runSync(ctx, paths, log, agentintel.PhaseState)
	case "sync-market":
		runSync(ctx, paths, log, agentintel.PhaseMarket)
	case "calculate":
		runCalculate(paths, log)
	case "generate":
		runGenerate(paths, log)
	case "", "all":
		runSync(ctx, paths, log, agentintel.PhaseAll)
		runCalculate(paths, log)
		runGenerate(paths, log)
	default:
		fmt.Fprintf(os.Stderr, "Usage: agent-intel [sync|sync-events|sync-state|sync-market|calculate|generate|all]\n")
		os.Exit(1)
	}
}

// runSync runs the requested sync phases.
// PhaseEvents / PhaseState run via agentintel.Syncer.SyncPhases.
// PhaseMarket runs via agentintel.MarketSyncer (separate component).
// Acquires a single sync lock for the whole run.
func runSync(ctx context.Context, paths agentintel.Paths, log *zap.Logger, phases agentintel.Phase) {
	rpcURL := os.Getenv("CHAIN_RPC_URL")
	if rpcURL == "" {
		fatal(log, "env", fmt.Errorf("CHAIN_RPC_URL not set"))
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		fatal(log, "dial rpc", err)
	}

	// Acquire the sync lock for the entire run. Released on exit (including fatal).
	release, err := agentintel.AcquireSyncLock(paths)
	if err != nil {
		fatal(log, "sync lock", err)
	}
	defer release()

	start := time.Now()

	// Blockchain phases (events + state).
	if phases&(agentintel.PhaseEvents|agentintel.PhaseState) != 0 {
		syncer := agentintel.NewSyncer(client, paths, log)
		if v := os.Getenv("AGENT_INTEL_STATE_MAX_AGE"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				syncer.SetStateMaxAge(d)
			} else {
				log.Warn("Invalid AGENT_INTEL_STATE_MAX_AGE, using default",
					zap.String("value", v), zap.Error(err))
			}
		}
		if os.Getenv("AGENT_INTEL_FORCE_STATE") == "1" {
			syncer.SetForceState(true)
		}
		if err := syncer.SyncPhases(ctx, phases); err != nil {
			fatal(log, "blockchain sync", err)
		}
	}

	// Market data phase.
	if phases&agentintel.PhaseMarket != 0 {
		meta, err := agentintel.LoadMeta(paths)
		if err != nil {
			fatal(log, "load meta", err)
		}
		marketSyncer := agentintel.NewMarketSyncer(paths, log)
		if err := marketSyncer.SyncMarketData(ctx, &meta); err != nil {
			fatal(log, "market sync", err)
		}
		if err := agentintel.SaveMeta(paths, meta); err != nil {
			fatal(log, "save meta", err)
		}
	}

	log.Info("Sync complete", zap.Duration("elapsed", time.Since(start)))
}

func runCalculate(paths agentintel.Paths, log *zap.Logger) {
	calc := agentintel.NewCalculator(paths, log)
	if err := calc.CalculateAll(); err != nil {
		fatal(log, "calculate", err)
	}
	log.Info("Calculation complete")
}

func runGenerate(paths agentintel.Paths, log *zap.Logger) {
	gen := agentintel.NewGenerator(paths, log)
	if err := gen.Generate(); err != nil {
		fatal(log, "generate", err)
	}
	log.Info("Site generated", zap.String("path", paths.Site))
}

type fatalErr struct {
	msg string
	err error
}

// fatal panics with a fatalErr so deferred cleanups (sync lock release, etc.)
// run during unwind. main() recovers and exits with code 1.
func fatal(_ *zap.Logger, msg string, err error) {
	panic(fatalErr{msg: msg, err: err})
}
