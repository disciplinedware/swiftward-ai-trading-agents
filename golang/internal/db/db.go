package db

import (
	"context"
	"fmt"
	"net/url"

	shopspringdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// NewPool creates a pgxpool connection pool and verifies connectivity.
func NewPool(ctx context.Context, connString string, log *zap.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Register shopspring/decimal codec so pgx can scan NUMERIC into decimal.Decimal
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		shopspringdecimal.Register(conn.TypeMap())
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	log.Info("Connected to trading database", zap.String("conn", redactConnString(connString)))
	return pool, nil
}

// redactConnString hides credentials from the connection string for logging.
func redactConnString(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "***"
	}
	u.User = nil
	return u.String()
}
