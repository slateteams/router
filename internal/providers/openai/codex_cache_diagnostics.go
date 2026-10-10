package openai

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"log/slog"
	"strconv"

	"weave-os/router/internal/observability"

	"github.com/tidwall/gjson"
)

// Fingerprints describe outbound JSON, not the backend's rendered tokens.
// A private per-client key prevents guessing prompt text from logged digests;
// epochs change when a client is recreated, so compare only within an epoch.
func (c *Client) logCodexCacheFingerprints(ctx context.Context, body []byte, affinity string) {
	log := observability.FromContext(ctx)
	if len(c.cacheFingerprintKey) == 0 || !log.Enabled(ctx, slog.LevelInfo) {
		return
	}
	fingerprint := func(domain string, parts ...string) string {
		mac := hmac.New(sha256.New, c.cacheFingerprintKey)
		var size [8]byte
		for _, part := range append([]string{domain}, parts...) {
			binary.BigEndian.PutUint64(size[:], uint64(len(part)))
			_, _ = mac.Write(size[:])
			_, _ = mac.Write([]byte(part))
		}
		return hex.EncodeToString(mac.Sum(nil))
	}
	input := gjson.GetBytes(body, "input")
	itemCount := 0
	if input.IsArray() {
		input.ForEach(func(_, _ gjson.Result) bool { itemCount++; return true })
	}
	options := make([]string, 0, 14)
	for _, path := range []string{"model", "reasoning", "text", "tool_choice", "parallel_tool_calls", "service_tier", "truncation"} {
		options = append(options, path, gjson.GetBytes(body, path).Raw)
	}
	attrs := []any{
		"fingerprint_version", 1,
		"fingerprint_epoch", fingerprint("epoch")[:16],
		"instructions_hmac", fingerprint("instructions", gjson.GetBytes(body, "instructions").Raw),
		"tools_hmac", fingerprint("tools", gjson.GetBytes(body, "tools").Raw),
		"request_options_hmac", fingerprint("request_options", options...),
		"session_affinity_hmac", fingerprint("session_affinity", affinity),
		"session_affinity_present", affinity != "",
		"input_items", itemCount,
		"input_bytes", len(input.Raw),
	}
	// Fixed checkpoints keep event size bounded as histories grow.
	for _, limit := range []int{1024, 4096, 16384, 65536, 262144} {
		prefix := input.Raw[:min(len(input.Raw), limit)]
		domain := "input_prefix_" + strconv.Itoa(limit)
		attrs = append(attrs, domain+"_hmac", fingerprint(domain, prefix))
	}
	log.InfoContext(ctx, "Codex cache request fingerprints", attrs...)
}
