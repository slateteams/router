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
func (c *Client) logCodexCacheFingerprints(ctx context.Context, body []byte, affinity, account string) {
	log := observability.FromContext(ctx)
	if len(c.cacheFingerprintKey) == 0 || !log.Enabled(ctx, slog.LevelInfo) {
		return
	}
	fingerprint := func(domain string, parts ...[]byte) string {
		mac := hmac.New(sha256.New, c.cacheFingerprintKey)
		var size [8]byte
		for _, part := range append([][]byte{[]byte(domain)}, parts...) {
			binary.BigEndian.PutUint64(size[:], uint64(len(part)))
			_, _ = mac.Write(size[:])
			_, _ = mac.Write(part)
		}
		return hex.EncodeToString(mac.Sum(nil))
	}
	input := gjson.GetBytes(body, "input")
	itemCount := 0
	if input.IsArray() {
		input.ForEach(func(_, _ gjson.Result) bool { itemCount++; return true })
	}
	options := make([][]byte, 0, 14)
	for _, path := range []string{"model", "reasoning", "text", "tool_choice", "parallel_tool_calls", "service_tier", "truncation"} {
		options = append(options, []byte(path), rawCodexCacheField(body, path))
	}
	attrs := []any{
		"fingerprint_version", 1,
		"fingerprint_epoch", fingerprint("epoch")[:16],
		"instructions_hmac", fingerprint("instructions", rawCodexCacheField(body, "instructions")),
		"tools_hmac", fingerprint("tools", rawCodexCacheField(body, "tools")),
		"request_options_hmac", fingerprint("request_options", options...),
		"session_affinity_hmac", fingerprint("session_affinity", []byte(affinity)),
		"session_affinity_present", affinity != "",
		"account_hmac", fingerprint("account", []byte(account)),
		"input_items", itemCount,
		"input_bytes", len(input.Raw),
	}
	// Fixed checkpoints keep event size bounded as histories grow.
	for _, limit := range []int{1024, 4096, 16384, 65536, 262144} {
		prefix := body[input.Index : input.Index+min(len(input.Raw), limit)]
		domain := "input_prefix_" + strconv.Itoa(limit)
		attrs = append(attrs, domain+"_hmac", fingerprint(domain, prefix))
	}
	log.InfoContext(ctx, "Codex cache request fingerprints", attrs...)
}

// Static field paths retain their offset into body, avoiding a second copy
// when hashing large instructions or tool schemas.
func rawCodexCacheField(body []byte, path string) []byte {
	value := gjson.GetBytes(body, path)
	return body[value.Index : value.Index+len(value.Raw)]
}
