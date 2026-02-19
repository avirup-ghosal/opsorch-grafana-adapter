package log

import (
	"context"
	"fmt"

	corelog "github.com/opsorch/opsorch-core/log"
	"github.com/opsorch/opsorch-core/schema"
)

// ProviderName is the registry key for this adapter.
const ProviderName = "loki"

// Provider is the Loki implementation of the core log provider.
type Provider struct {
	// add Loki HTTP client details here later
	baseURL string
}

// New constructs the Loki provider from the config.
func New(cfg map[string]any) (corelog.Provider, error) {
	// Basic placeholder for config parsing
	baseURL := "http://localhost:3100"
	if url, ok := cfg["url"].(string); ok {
		baseURL = url
	}

	return &Provider{
		baseURL: baseURL,
	}, nil
}

func init() {
	// Register this provider with OpsOrch Core so it can be routed to
	_ = corelog.RegisterProvider(ProviderName, New)
}

// Query executes a LogQL query against Loki.
func (p *Provider) Query(ctx context.Context, q schema.LogQuery) (schema.LogEntries, error) {
	// Just printing to the console for now to prove it's wired up
	fmt.Printf("DEBUG: Received LogQuery for Loki: %+v\n", q)

	// Return an empty LogEntries struct to satisfy the interface so the build passes
	return schema.LogEntries{}, nil
}