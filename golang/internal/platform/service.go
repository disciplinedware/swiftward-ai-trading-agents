package platform

// IService defines the interface for all services (agents, MCPs, etc.)
type IService interface {
	// Initialize performs cross-service discovery and setup
	Initialize() error

	// Start begins the service. Must block on <-ctx.Done()
	Start() error

	// Stop gracefully shuts down the service
	Stop() error
}
