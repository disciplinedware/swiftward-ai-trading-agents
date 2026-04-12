package platform

import (
	"context"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"ai-trading-agents/internal/config"
)

// ServiceContext holds shared dependencies injected into all services.
type ServiceContext struct {
	ctx    context.Context
	logger *zap.Logger
	cfg    *config.Config
	router chi.Router
	roles  []string
}

// NewServiceContext creates a new service context.
func NewServiceContext(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config.Config,
	router chi.Router,
	roles []string,
) *ServiceContext {
	return &ServiceContext{
		ctx:    ctx,
		logger: logger,
		cfg:    cfg,
		router: router,
		roles:  roles,
	}
}

func (c *ServiceContext) Context() context.Context { return c.ctx }
func (c *ServiceContext) Logger() *zap.Logger      { return c.logger }
func (c *ServiceContext) Config() *config.Config    { return c.cfg }
func (c *ServiceContext) Router() chi.Router        { return c.router }
func (c *ServiceContext) Roles() []string           { return c.roles }
