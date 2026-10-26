package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"weave-os/router/internal/websearch"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeSearchCapturePreservesStatusAndSafeHeaders(t *testing.T) {
	b := &nativeSearchBuffer{header: make(http.Header)}
	b.Header().Set("Retry-After", "17")
	b.Header().Set(HeaderRouterModel, "served-model")
	b.Header().Set("Set-Cookie", "synthetic-cookie")
	b.WriteHeader(http.StatusTooManyRequests)
	_, err := b.Write([]byte(`{"error":{"type":"rate_limit_error"}}`))
	require.NoError(t, err)
	b.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusTooManyRequests, b.status)
	rec := httptest.NewRecorder()
	copyNativeSearchHeaders(rec, b.header)
	assert.Equal(t, "17", rec.Header().Get("Retry-After"))
	assert.Equal(t, "served-model", rec.Header().Get(HeaderRouterModel))
	assert.Empty(t, rec.Header().Get("Set-Cookie"))
}

func TestNativeSearchCaptureBoundsUntrustedResponse(t *testing.T) {
	b := &nativeSearchBuffer{header: make(http.Header)}
	_, err := b.Write(bytes.Repeat([]byte("x"), nativeSearchResponseLimit))
	require.NoError(t, err)
	_, err = b.Write([]byte("x"))
	require.Error(t, err)
	assert.Equal(t, nativeSearchResponseLimit, b.body.Len())
}

func TestNativeSearchCaptureCancelsExcessToolOperations(t *testing.T) {
	cancelled := false
	b := &nativeSearchBuffer{header: make(http.Header), maxCalls: 1, calls: make(map[string]struct{}), cancel: func() { cancelled = true }}
	first := "data: " + `{"type":"response.output_item.added","output_index":1,"item":{"id":"search-first","type":"web_search_call"}}` + "\n\n"
	duplicate := "data: " + `{"type":"response.web_search_call.in_progress","output_index":0,"item_id":"search-first"}` + "\n\n"
	second := "data: " + `{"type":"response.web_search_call.in_progress","output_index":2}` + "\n\n"
	for _, chunk := range []string{first[:11], first[11:], duplicate} {
		_, err := b.Write([]byte(chunk))
		require.NoError(t, err)
	}
	_, err := b.Write([]byte(second))
	require.ErrorIs(t, err, websearch.ErrCallLimitExceeded)
	assert.True(t, cancelled)
}
