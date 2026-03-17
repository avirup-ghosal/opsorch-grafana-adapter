package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	corelog "github.com/opsorch/opsorch-core/log"
	"github.com/opsorch/opsorch-core/schema"

	adapter "github.com/opsorch/opsorch-grafana-adapter/log"
)

// rpcRequest represents an incoming command from the OpsOrch core.
type rpcRequest struct {
	Method  string          `json:"method"`
	Config  map[string]any  `json:"config"`
	Payload json.RawMessage `json:"payload"`
}

// rpcResponse represents the outgoing result sent back to OpsOrch core.
type rpcResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Global provider instance to maintain the HTTP client and connection pool.
var provider corelog.Provider

func main() {
	// The core system communicates with this plugin via standard I/O
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			writeErr(enc, err)
			return
		}

		// Ensure Loki provider is initialized with the correct config
		prov, err := ensureProvider(req.Config)
		if err != nil {
			writeErr(enc, err)
			continue
		}

		ctx := context.Background()

		// Route the incoming JSON-RPC method to the correct Go function
		switch req.Method {
		case "log.query":
			var query schema.LogQuery
			if err := json.Unmarshal(req.Payload, &query); err != nil {
				writeErr(enc, err)
				continue
			}

			res, err := prov.Query(ctx, query)
			write(enc, res, err)

		default:
			writeErr(enc, fmt.Errorf("unknown method: %s", req.Method))
		}
	}
}

func ensureProvider(cfg map[string]any) (corelog.Provider, error) {
	if provider != nil {
		return provider, nil
	}

	prov, err := adapter.New(cfg)
	if err != nil {
		return nil, err
	}

	provider = prov
	return provider, nil
}

// write handles sending successful payloads back to the core system.
func write(enc *json.Encoder, result any, err error) {
	if err != nil {
		writeErr(enc, err)
		return
	}
	_ = enc.Encode(rpcResponse{Result: result})
}

// writeErr safely packages errors so the core system doesn't crash if the plugin fails.
func writeErr(enc *json.Encoder, err error) {
	_ = enc.Encode(rpcResponse{Error: err.Error()})
}
