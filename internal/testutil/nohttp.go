package testutil

import (
	"fmt"
	"net/http"
	"testing"
)

// BlockExternalHTTP replaces http.DefaultTransport with a transport that
// rejects any non-localhost HTTP request. The original transport is restored
// via t.Cleanup.
func BlockExternalHTTP(t testing.TB) {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = &blockingTransport{original: original}
	t.Cleanup(func() { http.DefaultTransport = original })
}

type blockingTransport struct {
	original http.RoundTripper
}

func (b *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return b.original.RoundTrip(req)
	}
	return nil, fmt.Errorf("external HTTP call blocked in test: %s", req.URL.String())
}
