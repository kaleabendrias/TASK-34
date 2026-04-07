package api_tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Deterministic fixture builder. Each call returns a unique username so
// concurrent test packages don't collide.
var userCounter int64

func uniqueUsername(prefix string) string {
	n := atomic.AddInt64(&userCounter, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), n)
}

// Standard test password that satisfies the policy (12+ chars, upper, lower,
// digit, symbol). Reused everywhere so we don't accidentally break it.
const TestPassword = "Harbor@Test2026!"

// Client is a tiny HTTP client wrapper with a cookie jar so login state is
// kept across requests automatically.
type Client struct {
	HTTP *http.Client
}

func newClient(t *testing.T) *Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &Client{HTTP: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

// doRaw issues a request without retry. The returned body bytes are read in
// full and the response is closed.
//
// Default Accept header is application/json so middleware that redirects HTML
// clients to /auth/login does NOT trigger; tests want machine-readable
// responses by default. Tests that explicitly want HTML pass an Accept header.
func (c *Client) doRaw(t *testing.T, method, path string, body io.Reader, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil && headers["Content-Type"] == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if headers["Accept"] == "" {
		req.Header.Set("Accept", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

// doJSON marshals body to JSON and dispatches the request.
func (c *Client) doJSON(t *testing.T, method, path string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/json"
	return c.doRaw(t, method, path, reader, headers)
}

// mustJSON parses the body or fails the test. Returns a generic map.
func mustJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("parse json: %v\nbody=%s", err, string(body))
	}
	return m
}

// expectStatus fails the test if the actual status doesn't match.
func expectStatus(t *testing.T, resp *http.Response, body []byte, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status: want %d, got %d, body=%s", want, resp.StatusCode, string(body))
	}
}

// loginAsAdmin returns a logged-in client for the seeded harbormaster user.
func loginAsAdmin(t *testing.T) *Client {
	t.Helper()
	c := newClient(t)
	resp, body := c.doJSON(t, "POST", "/api/auth/login", map[string]string{
		"username": "harbormaster",
		"password": "Harbor@Works2026!",
	}, nil)
	expectStatus(t, resp, body, http.StatusOK)
	return c
}

// registerAndLogin creates a fresh user and returns a logged-in client.
func registerAndLogin(t *testing.T, prefix string) (*Client, string) {
	t.Helper()
	username := uniqueUsername(prefix)
	c := newClient(t)
	resp, body := c.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"username": username,
		"password": TestPassword,
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s: status=%d body=%s", username, resp.StatusCode, string(body))
	}
	resp, body = c.doJSON(t, "POST", "/api/auth/login", map[string]string{
		"username": username,
		"password": TestPassword,
	}, nil)
	expectStatus(t, resp, body, http.StatusOK)
	return c, username
}

// futureRFC3339 returns a UTC RFC3339 string n hours from now, truncated to
// the hour. Used to make booking timestamps deterministic.
func futureRFC3339(hours int) string {
	t := time.Now().UTC().Add(time.Duration(hours) * time.Hour).Truncate(time.Hour)
	return t.Format(time.RFC3339)
}

// idemKey returns a unique idempotency key for a test invocation.
func idemKey(scope string) string {
	return fmt.Sprintf("idem-%s-%d", scope, time.Now().UnixNano())
}

// Resource UUIDs available from the seed file.
const (
	SeedSlipA1     = "aaaa1111-0000-0000-0000-000000000001"
	SeedSlipA2     = "aaaa1111-0000-0000-0000-000000000002"
	SeedMooringM7  = "aaaa1111-0000-0000-0000-000000000003"
	SeedConfRoom   = "bbbb2222-0000-0000-0000-000000000001"
	SeedSunsetDeck = "bbbb2222-0000-0000-0000-000000000002"
)

// containsAll asserts every needle appears somewhere in the haystack body.
// Used for ad-hoc text assertions when full JSON parsing would be heavyweight.
func containsAll(t *testing.T, body []byte, needles ...string) {
	t.Helper()
	s := string(body)
	for _, n := range needles {
		if !strings.Contains(s, n) {
			t.Errorf("body missing %q\n--- body ---\n%s", n, s)
		}
	}
}
