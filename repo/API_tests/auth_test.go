package api_tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestAuth_RegistrationWeakPasswordRejected(t *testing.T) {
	c := newClient(t)
	resp, body := c.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"username": uniqueUsername("weak"),
		"password": "short",
	}, nil)
	expectStatus(t, resp, body, http.StatusBadRequest)
	containsAll(t, body, "password does not meet policy")
}

func TestAuth_RegistrationDuplicateRejected(t *testing.T) {
	username := uniqueUsername("dup")
	c := newClient(t)
	resp, body := c.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"username": username, "password": TestPassword,
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	resp, body = c.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"username": username, "password": TestPassword,
	}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate, got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestAuth_LoginInvalidCredentials(t *testing.T) {
	c := newClient(t)
	resp, body := c.doJSON(t, "POST", "/api/auth/login", map[string]string{
		"username": "no-such-user-99999",
		"password": "wrong-password-1A!",
	}, nil)
	expectStatus(t, resp, body, http.StatusUnauthorized)
}

func TestAuth_CaptchaRequiredFromThirdAttemptThenLockout(t *testing.T) {
	c, username := registerAndLogin(t, "lockme")
	// Create a fresh client with no cookies for the negative attempts.
	intruder := newClient(t)

	// 1st + 2nd wrong attempts → bare 401
	for i := 0; i < 2; i++ {
		resp, _ := intruder.doJSON(t, "POST", "/api/auth/login", map[string]string{
			"username": username, "password": "wrong-pw-1A!",
		}, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, resp.StatusCode)
		}
	}

	// 3rd attempt with no captcha → 401 + captcha challenge.
	resp, body := intruder.doJSON(t, "POST", "/api/auth/login", map[string]string{
		"username": username, "password": "wrong-pw-1A!",
	}, nil)
	expectStatus(t, resp, body, http.StatusUnauthorized)
	if !strings.Contains(string(body), "captcha required") {
		t.Fatalf("expected captcha required, got %s", body)
	}
	// Drive lockout: 3 more attempts with solved captcha but wrong password.
	for i := 0; i < 3; i++ {
		// Mint a captcha and submit the right answer.
		resp, body := intruder.doJSON(t, "GET", "/api/auth/captcha", nil, nil)
		expectStatus(t, resp, body, http.StatusOK)
		ch := mustJSON(t, body)
		token, _ := ch["token"].(string)
		question, _ := ch["question"].(string)
		var a, b int
		// "What is X + Y?" — parse cheaply.
		_, _ = parseSum(question, &a, &b)
		answer := a + b

		resp, body = intruder.doJSON(t, "POST", "/api/auth/login", map[string]any{
			"username":       username,
			"password":       "still-wrong-1A!",
			"captcha_token":  token,
			"captcha_answer": itoa(answer),
		}, nil)
		// Expect either 401 (invalid credentials) or 423 (locked) once we hit
		// the threshold.
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusLocked {
			t.Fatalf("captcha attempt %d unexpected status %d body=%s", i+1, resp.StatusCode, body)
		}
	}

	// At this point the user should be locked. Even the *correct* password
	// must return 423.
	resp, body = intruder.doJSON(t, "POST", "/api/auth/login", map[string]string{
		"username": username, "password": TestPassword,
	}, nil)
	if resp.StatusCode != http.StatusLocked {
		t.Fatalf("expected 423 lockout for correct password, got %d body=%s", resp.StatusCode, body)
	}

	// Original logged-in client still has its cookie; sanity-check it. The
	// session is independent of password attempts.
	resp, body = c.doJSON(t, "GET", "/api/auth/me", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
}

func TestAuth_LogoutClearsSession(t *testing.T) {
	c, _ := registerAndLogin(t, "logout")
	resp, body := c.doJSON(t, "POST", "/api/auth/logout", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	resp, _ = c.doJSON(t, "GET", "/api/auth/me", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", resp.StatusCode)
	}
}

func TestAuth_HTMLPagesRender(t *testing.T) {
	for _, p := range []string{"/", "/auth/login", "/auth/register"} {
		c := newClient(t)
		resp, body := c.doRaw(t, "GET", p, nil, map[string]string{"Accept": "text/html"})
		expectStatus(t, resp, body, http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s content-type = %q", p, ct)
		}
	}
}

// ---- tiny helpers (kept inline so the suite has no extra deps) ----

func parseSum(s string, a, b *int) (bool, error) {
	// Format is "What is X + Y?". Pull the two ints with a hand parser.
	parts := strings.Fields(s)
	if len(parts) < 5 {
		return false, nil
	}
	x, err := atoi(parts[2])
	if err != nil {
		return false, err
	}
	y, err := atoi(strings.TrimSuffix(parts[4], "?"))
	if err != nil {
		return false, err
	}
	*a, *b = x, y
	return true, nil
}

func atoi(s string) (int, error) {
	n := 0
	sign := 1
	if strings.HasPrefix(s, "-") {
		sign = -1
		s = s[1:]
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errBadInt
		}
		n = n*10 + int(r-'0')
	}
	return sign * n, nil
}

var errBadInt = &simpleErr{"bad int"}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }
