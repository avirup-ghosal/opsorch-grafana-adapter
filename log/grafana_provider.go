package log

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	corelog "github.com/opsorch/opsorch-core/log"
	"github.com/opsorch/opsorch-core/schema"
)

// ProviderName is the registry key for this adapter.
const ProviderName = "grafana"

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
	u.Path = path.Join(u.Path, "/loki/api/v1/query_range")

	logQL := buildLogQL(&q)

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
func buildLogQL(q *schema.LogQuery) string {
	// Safe fallback if the entire query object is nil
	if q == nil {
		return `{service_name=~".+"}`
	}

	var labelSelectors []string
	var lineFilters []string

	// Inject Scope (Service & Environment)
	if q.Scope.Service != "" {
		labelSelectors = append(labelSelectors, fmt.Sprintf(`service=%q`, q.Scope.Service))
	}
	if q.Scope.Environment != "" {
		labelSelectors = append(labelSelectors, fmt.Sprintf(`env=%q`, q.Scope.Environment))
	}

	// Inject Metadata (Custom key-value pairs)
	for key, value := range q.Metadata {
		strVal := fmt.Sprintf("%v", value)
		safeKey := sanitizeLabelName(key)
		labelSelectors = append(labelSelectors, fmt.Sprintf(`%s=%q`, safeKey, strVal))
	}

	// Inject structured Expression filters securely
	if q.Expression != nil {
		for _, filter := range q.Expression.Filters {
			field := sanitizeLabelName(filter.Field)
			val := fmt.Sprintf("%v", filter.Value)

			switch filter.Operator {
			case "regex":
				// Allow regex, escape quotes & backslashes so it doesn't break the LogQL string literal
				safeVal := strings.ReplaceAll(val, `\`, `\\`)
				safeVal = strings.ReplaceAll(safeVal, `"`, `\"`)
				lineFilters = append(lineFilters, fmt.Sprintf(`%s=~"%s"`, field, safeVal))
			case "contains":
				// QuoteMeta prevents regex injection. Then we escape quotes for the LogQL string literal.
				safeVal := regexp.QuoteMeta(val)
				safeVal = strings.ReplaceAll(safeVal, `\`, `\\`)
				safeVal = strings.ReplaceAll(safeVal, `"`, `\"`)
				lineFilters = append(lineFilters, fmt.Sprintf(`%s=~".*%s.*"`, field, safeVal))
			case "!=":
				lineFilters = append(lineFilters, fmt.Sprintf(`%s!=%q`, field, val))
			case "=":
				fallthrough
			default:
				lineFilters = append(lineFilters, fmt.Sprintf(`%s=%q`, field, val))
			}
		}
	}

	logQL := ""
	if len(labelSelectors) > 0 {
		logQL = fmt.Sprintf("{%s}", strings.Join(labelSelectors, ", "))
	} else {
		logQL = `{service_name=~".+"}`
	}

	if len(lineFilters) > 0 {
		logQL += " | json"
		for _, filter := range lineFilters {
			logQL += fmt.Sprintf(" | %s", filter)
		}
	}

	if q.Expression != nil {
		// Search is already safe because it uses %q
		if q.Expression.Search != "" {
			logQL += fmt.Sprintf(" |= %q", q.Expression.Search)
		}

		if len(q.Expression.SeverityIn) > 0 {
			// Prevent regex injection in severity mapping
			var safeSeverities []string
			for _, sev := range q.Expression.SeverityIn {
				safeSeverities = append(safeSeverities, regexp.QuoteMeta(sev))
			}
			severities := strings.Join(safeSeverities, "|")

			// Format safely into a LogQL double-quoted regex string
			safeRegex := fmt.Sprintf("(?i)(%s)", severities)
			safeRegex = strings.ReplaceAll(safeRegex, `\`, `\\`)
			safeRegex = strings.ReplaceAll(safeRegex, `"`, `\"`)

			logQL += fmt.Sprintf(` |~ "%s"`, safeRegex)
		}
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

// sanitizeLabelName enforces Loki label naming rules.
// It matches the regex [a-zA-Z_:][a-zA-Z0-9_:]* and prevents __internal__ collisions.
func sanitizeLabelName(key string) string {
	var sb strings.Builder
	for _, r := range key {
		// Allow letters, numbers, underscores, and colons
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}

	res := sb.String()

	if len(res) > 0 && res[0] >= '0' && res[0] <= '9' {
		res = "_" + res
	}

	// Prevent Grafana internal label collision (cannot start AND end with __)
	if strings.HasPrefix(res, "__") && strings.HasSuffix(res, "__") {
		res = strings.TrimSuffix(res, "_")
	}

	return res
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
