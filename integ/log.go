//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/opsorch/opsorch-core/schema"

	adapter "github.com/opsorch/opsorch-grafana-adapter/log"
)

func seedTestLog(lokiURL string) {
	log.Println(" Seeding test data into Loki...")

	now := time.Now().UnixNano()
	payload := fmt.Sprintf(`{"streams": [{"stream": {"job": "integ-test", "app": "opsorch", "service": "payment-api", "env": "prod", "cluster": "us-east-1"}, "values": [[ "%d", "fatal error: database connection lost" ]]}]}`, now)

	req, err := http.NewRequest("POST", lokiURL+"/loki/api/v1/push", strings.NewReader(payload))
	if err != nil {
		log.Fatalf(" Failed to create seed request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf(" Failed to seed Loki: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Fatalf(" Loki rejected seed data with status: %d", resp.StatusCode)
	}

	log.Println(" Waiting 5 seconds for Loki to index the log...")
	time.Sleep(5 * time.Second)
}

func main() {
	url := os.Getenv("LOKI_URL")
	if url == "" {
		url = "http://localhost:3100"
		log.Printf("LOKI_URL not set, defaulting to %s", url)
	}

	seedTestLog(url)

	cfg := map[string]any{
		"url": url,
	}

	provider, err := adapter.New(cfg)
	if err != nil {
		log.Fatalf(" FATAL: Failed to create provider: %v", err)
	}

	ctx := context.Background()

	startWindow := time.Now().Add(-24 * time.Hour)
	endWindow := time.Now().Add(24 * time.Hour)

	tests := []struct {
		name     string
		query    schema.LogQuery
		validate func(schema.LogEntries) error
	}{
		{
			name: "Basic Query (all logs fallback)",
			query: schema.LogQuery{
				Expression: &schema.LogExpression{},
				Start:      startWindow,
				End:        endWindow,
				Limit:      10,
			},
			validate: func(res schema.LogEntries) error {
				if len(res.Entries) == 0 {
					return fmt.Errorf("expected at least 1 log entry, got 0")
				}
				return nil
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
				Start: startWindow,
				End:   endWindow,
				Limit: 10,
			},
			validate: func(res schema.LogEntries) error {
				if len(res.Entries) == 0 {
					return fmt.Errorf("expected at least 1 log entry, got 0")
				}
				for _, entry := range res.Entries {
					if entry.Labels["app"] != "opsorch" {
						return fmt.Errorf("expected label app=opsorch, got %v", entry.Labels["app"])
					}
				}
				return nil
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
				Start: startWindow,
				End:   endWindow,
				Limit: 10,
			},
			validate: func(res schema.LogEntries) error {
				if len(res.Entries) == 0 {
					return fmt.Errorf("expected at least 1 log entry, got 0")
				}
				for _, entry := range res.Entries {
					if !strings.Contains(strings.ToLower(entry.Message), "error") {
						return fmt.Errorf("expected message to contain 'error', got: %s", entry.Message)
					}
				}
				return nil
			},
		},
		{
			name: "Query with Scope and Metadata",
			query: schema.LogQuery{
				Scope: schema.QueryScope{
					Service:     "payment-api",
					Environment: "prod",
				},
				Metadata: map[string]any{
					"cluster": "us-east-1",
				},
				Start: startWindow,
				End:   endWindow,
				Limit: 10,
			},
			validate: func(res schema.LogEntries) error {
				if len(res.Entries) == 0 {
					return fmt.Errorf("expected at least 1 log entry, got 0")
				}
				for _, entry := range res.Entries {
					if entry.Labels["service"] != "payment-api" {
						return fmt.Errorf("expected label service=payment-api, got %v", entry.Labels["service"])
					}
					if entry.Labels["env"] != "prod" {
						return fmt.Errorf("expected label env=prod, got %v", entry.Labels["env"])
					}
					if entry.Labels["cluster"] != "us-east-1" {
						return fmt.Errorf("expected label cluster=us-east-1, got %v", entry.Labels["cluster"])
					}
				}
				return nil
			},
		},
	}

	log.Println(" Starting strict integration tests...")

	for _, tt := range tests {
		log.Printf("Running: %s", tt.name)
		results, err := provider.Query(ctx, tt.query)
		if err != nil {
			log.Fatalf(" API CALL FAILED: %v", err)
		}

		if tt.validate != nil {
			if err := tt.validate(results); err != nil {
				log.Fatalf(" ASSERTION FAILED: %v", err)
			}
		}

		log.Printf(" Passed (%d entries returned)", len(results.Entries))
	}

	log.Println(" All integration tests passed successfully!")
}
