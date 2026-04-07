package api_tests

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// baseURL points at the cover-instrumented binary started inside the test
// container by entrypoint.sh. The default works when tests run in that
// container; APP_URL can override it for ad-hoc local runs.
var baseURL = func() string {
	if v := os.Getenv("APP_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8080"
}()

func TestMain(m *testing.M) {
	if err := waitForReady(60 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "app not ready at %s: %v\n", baseURL, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = context.DeadlineExceeded
	}
	return lastErr
}
