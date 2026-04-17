package testutil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlockExternalHTTP_LocalhostAllowed(t *testing.T) {
	// Start a local test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	BlockExternalHTTP(t)

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("localhost request should succeed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestBlockExternalHTTP_ExternalBlocked(t *testing.T) {
	BlockExternalHTTP(t)

	_, err := http.Get("http://example.com/test")
	if err == nil {
		t.Fatal("expected error for external HTTP call")
	}
	if !strings.Contains(err.Error(), "external HTTP call blocked in test:") {
		t.Errorf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("error should contain the blocked URL: %v", err)
	}
}

func TestBlockExternalHTTP_RestoredAfterCleanup(t *testing.T) {
	original := http.DefaultTransport

	// Run in a sub-test so cleanup triggers
	t.Run("block", func(t *testing.T) {
		BlockExternalHTTP(t)
		if http.DefaultTransport == original {
			t.Error("transport should be replaced during test")
		}
	})

	if http.DefaultTransport != original {
		t.Error("transport should be restored after cleanup")
	}
}
