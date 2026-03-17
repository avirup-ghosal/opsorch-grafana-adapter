package log

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opsorch/opsorch-core/schema"
)

func TestNewLokiProvider(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"url": "http://localhost:3100"},
			wantErr: false,
		},
		{
			name:    "empty config uses default",
			config:  map[string]any{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLokiProvider_Query(t *testing.T) {
	defaultStart := time.Unix(1708990000, 0)
	defaultEnd := time.Unix(1708993600, 0)

	tests := []struct {
		name           string
		query          schema.LogQuery
		mockResponse   string
		mockStatusCode int
		expectedQuery  string
		wantEntries    int
		wantErr        bool
		validate       func(*testing.T, schema.LogEntries)
	}{
		{
			name: "basic query with filters and search",
			query: schema.LogQuery{
				Expression: &schema.LogExpression{
					Filters: []schema.LogFilter{
						{Field: "app", Operator: "=", Value: "frontend"},
					},
					Search: "error connecting",
				},
				Start: defaultStart,
				End:   defaultEnd,
				Limit: 100,
			},
			mockResponse: `{
                "status": "success",
                "data": {
                    "resultType": "streams",
                    "result": [
                        {
                            "stream": { "app": "frontend" },
                            "values": [ [ "1708990000000000000", "error connecting to db" ] ]
                        }
                    ]
                }
            }`,
			expectedQuery: `{app="frontend"} |= "error connecting"`,
			wantEntries:   1,
			validate: func(t *testing.T, res schema.LogEntries) {
				if res.Entries[0].Labels["app"] != "frontend" {
					t.Errorf("got label %v", res.Entries[0].Labels["app"])
				}
				if res.Entries[0].Message != "error connecting to db" {
					t.Errorf("got message %s", res.Entries[0].Message)
				}
			},
		},
		{
			name: "empty expression returns safe default",
			query: schema.LogQuery{
				Start: defaultStart,
				End:   defaultEnd,
			},
			mockResponse: `{
                "status": "success",
                "data": { "resultType": "streams", "result": [] }
            }`,
			expectedQuery: `{job=~".+"}`,
			wantEntries:   0,
		},
		{
			name: "query with scope and metadata",
			query: schema.LogQuery{
				Scope: schema.QueryScope{
					Service:     "api-gateway",
					Environment: "prod",
				},
				Metadata: map[string]any{
					"cluster": "us-east-1",
				},
				Start: defaultStart,
				End:   defaultEnd,
			},
			mockResponse: `{
                "status": "success",
                "data": { "resultType": "streams", "result": [] }
            }`,
			expectedQuery: `{service="api-gateway", env="prod", cluster="us-east-1"}`,
			wantEntries:   0,
		},
		{
			name: "api error handles gracefully",
			query: schema.LogQuery{
				Expression: &schema.LogExpression{
					Filters: []schema.LogFilter{{Field: "app", Operator: "=", Value: "backend"}},
				},
			},
			mockStatusCode: 400,
			mockResponse:   `{"status":"error","error":"bad request"}`,
			wantErr:        true,
		},
		{
			name: "malformed json response",
			query: schema.LogQuery{
				Expression: &schema.LogExpression{
					Filters: []schema.LogFilter{{Field: "app", Operator: "=", Value: "backend"}},
				},
			},
			mockStatusCode: 200,
			mockResponse:   `this is not json`,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Spin up mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify endpoint
				if r.URL.Path != "/loki/api/v1/query_range" {
					t.Errorf("Expected path /loki/api/v1/query_range, got %s", r.URL.Path)
				}

				// Verify the query string
				q := r.URL.Query().Get("query")
				if tt.expectedQuery != "" && q != tt.expectedQuery {
					t.Errorf("Expected query '%s', got '%s'", tt.expectedQuery, q)
				}

				// Return mock response
				w.Header().Set("Content-Type", "application/json")
				if tt.mockStatusCode != 0 {
					w.WriteHeader(tt.mockStatusCode)
				} else {
					w.WriteHeader(http.StatusOK)
				}
				w.Write([]byte(tt.mockResponse))
			}))
			defer server.Close()

			// Initialize the provider with the mock server URL
			provider, err := New(map[string]any{"url": server.URL})
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			// Execute the query
			result, err := provider.Query(context.Background(), tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("Query() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Validate results
			if !tt.wantErr {
				if len(result.Entries) != tt.wantEntries {
					t.Errorf("Query() got %d entries, want %d", len(result.Entries), tt.wantEntries)
				}
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}
