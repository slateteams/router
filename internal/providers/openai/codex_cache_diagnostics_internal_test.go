package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"weave-os/router/internal/observability"
	"weave-os/router/internal/providers"
	"weave-os/router/internal/router"

	"github.com/stretchr/testify/require"
)

func TestCodexCacheDiagnosticsFingerprintChangesWithoutLoggingContent(t *testing.T) {
	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveSyntheticCodexQuota(w, r) {
			return
		}
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer upstream.Close()
	client := NewClient("synthetic-paid-key", upstream.URL)
	client.SetCodexBaseURL(upstream.URL)
	base := map[string]any{
		"model": "gpt-6.1-sol", "instructions": "PRIVATE_INSTRUCTIONS_MARKER",
		"tools":     []any{map[string]any{"type": "function", "name": "synthetic", "description": "PRIVATE_TOOL_MARKER", "parameters": map[string]any{"type": "object"}}},
		"input":     []any{map[string]any{"role": "user", "content": strings.Repeat("PRIVATE_HISTORY_MARKER ", 13000)}},
		"reasoning": map[string]any{"effort": "high"}, "prompt_cache_key": "PRIVATE_AFFINITY_MARKER", "stream": true,
	}
	var output bytes.Buffer
	ctx := observability.WithLogger(codexCtx("PRIVATE_BEARER_MARKER", "PRIVATE_ACCOUNT_MARKER"), slog.New(slog.NewJSONHandler(&output, nil)).With("request_id", "synthetic-request", "session_key", "synthetic-thread"))
	proxy := func(body map[string]any) map[string]any {
		t.Helper()
		output.Reset()
		data, err := json.Marshal(body)
		require.NoError(t, err)
		require.NoError(t, client.Proxy(ctx, router.Decision{Model: "gpt-6.1-sol", Provider: providers.ProviderOpenAI}, providers.PreparedRequest{Endpoint: providers.EndpointResponses, Body: data}, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/messages", nil)))
		require.NotEmpty(t, output.String(), "subscription dispatch must emit fingerprint diagnostics")
		require.JSONEq(t, string(data), string(receivedBody), "diagnostics must not rewrite the outbound body")
		lines := strings.Split(strings.TrimSpace(output.String()), "\n")
		require.Len(t, lines, 1, "subscription dispatch must emit one bounded fingerprint event")
		for _, marker := range []string{"PRIVATE_INSTRUCTIONS_MARKER", "PRIVATE_TOOL_MARKER", "PRIVATE_HISTORY_MARKER", "PRIVATE_AFFINITY_MARKER", "PRIVATE_BEARER_MARKER", "PRIVATE_ACCOUNT_MARKER", "synthetic-paid-key", "OTHER_PRIVATE_ACCOUNT_MARKER"} {
			require.NotContains(t, output.String(), marker)
		}
		require.Less(t, output.Len(), 3000, "diagnostic size must not grow with conversation size")
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[0]), &event))
		return event
	}
	first := proxy(base)
	require.Equal(t, "synthetic-request", first["request_id"])
	require.Equal(t, "synthetic-thread", first["session_key"])
	repeated := proxy(base)
	for _, key := range []string{"fingerprint_epoch", "instructions_hmac", "tools_hmac", "request_options_hmac", "session_affinity_hmac", "account_hmac", "input_prefix_1024_hmac", "input_prefix_4096_hmac", "input_prefix_16384_hmac", "input_prefix_65536_hmac", "input_prefix_262144_hmac"} {
		require.NotEmpty(t, first[key], key)
		require.Equal(t, first[key], repeated[key], key)
	}
	base["instructions"] = "CHANGED_INSTRUCTIONS_MARKER"
	changed := proxy(base)
	require.NotEqual(t, first["instructions_hmac"], changed["instructions_hmac"])
	require.Equal(t, first["tools_hmac"], changed["tools_hmac"])
	base["tools"] = []any{map[string]any{"type": "function", "name": "changed_tool"}}
	changedTools := proxy(base)
	require.NotEqual(t, changed["tools_hmac"], changedTools["tools_hmac"])
	require.Equal(t, changed["instructions_hmac"], changedTools["instructions_hmac"])
	base["reasoning"] = map[string]any{"effort": "xhigh"}
	changedOptions := proxy(base)
	require.NotEqual(t, changedTools["request_options_hmac"], changedOptions["request_options_hmac"])
	base["prompt_cache_key"] = "changed-affinity"
	changedAffinity := proxy(base)
	require.NotEqual(t, changedOptions["session_affinity_hmac"], changedAffinity["session_affinity_hmac"])
	// The first item exceeds every checkpoint; appending preserves those bytes.
	base["input"] = []any{map[string]any{"role": "user", "content": strings.Repeat("PRIVATE_HISTORY_MARKER ", 13000)}, map[string]any{"role": "user", "content": "appended turn"}}
	appended := proxy(base)
	require.Equal(t, changedAffinity["input_prefix_262144_hmac"], appended["input_prefix_262144_hmac"])
	base["input"] = []any{map[string]any{"role": "user", "content": "rewritten early history"}}
	rewritten := proxy(base)
	require.NotEqual(t, appended["input_prefix_1024_hmac"], rewritten["input_prefix_1024_hmac"])

	client = NewClient("synthetic-paid-key", upstream.URL)
	client.SetCodexBaseURL(upstream.URL)
	newEpoch := proxy(base)
	require.NotEqual(t, rewritten["fingerprint_epoch"], newEpoch["fingerprint_epoch"])
	require.NotEqual(t, rewritten["instructions_hmac"], newEpoch["instructions_hmac"])

	ctx = observability.WithLogger(codexCtx("PRIVATE_BEARER_MARKER", "OTHER_PRIVATE_ACCOUNT_MARKER"), slog.New(slog.NewJSONHandler(&output, nil)))
	changedAccount := proxy(base)
	require.NotEqual(t, newEpoch["account_hmac"], changedAccount["account_hmac"])
	require.Equal(t, newEpoch["instructions_hmac"], changedAccount["instructions_hmac"])

	// Paid requests must not acquire subscription-specific logging.
	output.Reset()
	paidCtx := observability.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&output, nil)))
	data, err := json.Marshal(base)
	require.NoError(t, err)
	require.NoError(t, client.Proxy(paidCtx, router.Decision{Model: "gpt-6.1-sol", Provider: providers.ProviderOpenAI}, providers.PreparedRequest{Endpoint: providers.EndpointResponses, Body: data}, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/messages", nil)))
	require.NotContains(t, output.String(), "Codex cache request fingerprints")
}
