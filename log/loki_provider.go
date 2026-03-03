package log

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	corelog "github.com/opsorch/opsorch-core/log"
	"github.com/opsorch/opsorch-core/schema"
)

// ProviderName is the registry key for this adapter.
const ProviderName = "loki"

// Provider is the Loki implementation of the core log provider.
type Provider struct {
	baseURL string
	client  *http.Client
}

func init() {
	// Register this provider with OpsOrch Core for dynamic routing
	_ = corelog.RegisterProvider(ProviderName, New)
}

// New constructs the Loki provider from the config dictionary.
func New(cfg map[string]any) (corelog.Provider, error) {
	// Basic placeholder for config parsing
	baseURL := "http://localhost:3100"
	if url, ok := cfg["url"].(string); ok {
		baseURL = url
	}
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
	}

	return &Provider{
		baseURL: baseURL,
		client:  httpClient,
	}, nil
}

// buildQueryURL securely generates the Loki API endpoint with required parameters.
func (p *Provider) buildQueryURL(q schema.LogQuery) (string, error) {
	u, err := url.Parse(p.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	u.Path = "/loki/api/v1/query_range"

	logQL := buildLogQL(q.Expression)

	params := url.Values{}
	params.Add("query", logQL)

	if !q.Start.IsZero() {
		params.Add("start", strconv.FormatInt(q.Start.UnixNano(), 10))
	}
	if !q.End.IsZero() {
		params.Add("end", strconv.FormatInt(q.End.UnixNano(), 10))
	}
	if q.Limit > 0 {
		params.Add("limit", strconv.Itoa(q.Limit))
	}

	u.RawQuery = params.Encode()
	return u.String(), nil
}

// buildLogQL translates the generic OpsOrch LogExpression into a Loki LogQL query.
func buildLogQL(expr *schema.LogExpression) string {
	// Loki requires at least one label matcher; use a safe fallback if empty
	if expr == nil {
		return `{job=~".+"}`
	}

	var labelSelectors []string

	// 1. Map the structured filters into Loki labels: {field="value"}
	for _, filter := range expr.Filters {
		op := filter.Operator
		if op == "contains" || op == "regex" {
			op = "=~"
		}
		// Format: field="value"
		labelSelectors = append(labelSelectors, fmt.Sprintf(`%s%s%q`, filter.Field, op, filter.Value))
	}

	logQL := "{}"
	if len(labelSelectors) > 0 {
		logQL = fmt.Sprintf("{%s}", strings.Join(labelSelectors, ", "))
	} else {
		logQL = `{job=~".+"}` // Loki requires at least one label matcher
	}

	if expr.Search != "" {
		logQL += fmt.Sprintf(" |= %q", expr.Search)
	}

	if len(expr.SeverityIn) > 0 {
		severities := strings.Join(expr.SeverityIn, "|")
		logQL += fmt.Sprintf(" |~ `(?i)(%s)`", severities)
	}

	return logQL
}

// Query executes a LogQL query against Loki and maps the results to OpsOrch format.
func (p *Provider) Query(ctx context.Context, q schema.LogQuery) (schema.LogEntries, error) {
	queryURL, err := p.buildQueryURL(q)
	if err != nil {
		return schema.LogEntries{}, fmt.Errorf("failed to build Loki URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return schema.LogEntries{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return schema.LogEntries{}, fmt.Errorf("loki network request failed: %w", err)
	}
	defer resp.Body.Close() // prevent memory leaks

	if resp.StatusCode != http.StatusOK {
		return schema.LogEntries{}, fmt.Errorf("loki API returned status code %d", resp.StatusCode)
	}

	var lokiResp lokiResponse
	if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
		return schema.LogEntries{}, fmt.Errorf("failed to decode loki JSON response: %w", err)
	}

	var parsedEntries []schema.LogEntry

	for _, stream := range lokiResp.Data.Result {
		for _, val := range stream.Values {
			// Loki values are arrays: [ "UnixEpochNanoString", "Log Message" ]
			if len(val) != 2 {
				continue // Skip malformed log lines
			}

			tsNano, parseErr := strconv.ParseInt(val[0], 10, 64)
			if parseErr != nil {
				continue // Skip if the timestamp is broken
			}
			timestamp := time.Unix(0, tsNano)

			// Create the normalized OpsOrch log entry
			entry := schema.LogEntry{
				Timestamp: timestamp,
				Message:   val[1],
				Labels:    stream.Stream, // Attach the Loki labels directly to the entry
			}

			parsedEntries = append(parsedEntries, entry)
		}
	}

	return schema.LogEntries{
		Entries: parsedEntries,
	}, nil
}

// lokiResponse represents the top-level JSON wrapper from the Loki API.
type lokiResponse struct {
	Status string   `json:"status"`
	Data   lokiData `json:"data"`
}

// lokiData holds the actual query results.
type lokiData struct {
	ResultType string       `json:"resultType"`
	Result     []lokiStream `json:"result"`
}

// lokiStream represents a single log stream and its associated values.
type lokiStream struct {
	Stream map[string]string `json:"stream"` // The labels (e.g., app="frontend")
	Values [][]string        `json:"values"` // The logs: [0] is the Unix timestamp (string), [1] is the log line
}
