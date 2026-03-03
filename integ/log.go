//go:build ignore

package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/opsorch/opsorch-core/schema"

	adapter "github.com/opsorch/opsorch-grafana-adapter/log"
)

func main() {
	url := os.Getenv("LOKI_URL")
	if url == "" {
		url = "http://localhost:3100"
		log.Printf("LOKI_URL not set, defaulting to %s", url)
	}

	cfg := map[string]any{
		"url": url,
	}

	// Initialize the provider
	provider, err := adapter.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()

	// Test Scenarios
	tests := []struct {
		name  string
		query schema.LogQuery
	}{
		{
			name: "Basic Query (all logs fallback)",
			query: schema.LogQuery{
				Expression: &schema.LogExpression{},
				Start:      time.Now().Add(-1 * time.Hour),
				End:        time.Now(),
				Limit:      10,
			},
		},
		{
			name: "Query with Filters ({app='opsorch'})",
			query: schema.LogQuery{
				Expression: &schema.LogExpression{
					Filters: []schema.LogFilter{
						{Field: "app", Operator: "=", Value: "opsorch"},
					},
				},
				Start: time.Now().Add(-1 * time.Hour),
				End:   time.Now(),
				Limit: 10,
			},
		},
		{
			name: "Query with Search ( |= 'error' )",
			query: schema.LogQuery{
				Expression: &schema.LogExpression{
					Filters: []schema.LogFilter{
						{Field: "app", Operator: "=", Value: "opsorch"},
					},
					Search: "error",
				},
				Start: time.Now().Add(-1 * time.Hour),
				End:   time.Now(),
				Limit: 10,
			},
		},
	}

	// Execute scenarios
	for _, tt := range tests {
		log.Printf("Running Test: %s...", tt.name)
		results, err := provider.Query(ctx, tt.query)
		if err != nil {
			log.Printf("  FAILED: %v", err)
			continue
		}

		log.Printf("  Success: returned %d log entries", len(results.Entries))

		for i, entry := range results.Entries {
			if i < 3 { // Limit output to prevent spamming the terminal
				log.Printf("    Entry %d: [%s] Labels: %v | Message: %s",
					i,
					entry.Timestamp.Format(time.RFC3339),
					entry.Labels,
					entry.Message,
				)
			}
		}

		if len(results.Entries) > 3 {
			log.Printf("    ... and %d more", len(results.Entries)-3)
		}
	}

	log.Println("\nIntegration tests complete!")
}
