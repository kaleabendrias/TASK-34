package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

const (
	WebhookMaxAttempts = 5
)

// AllowedWebhookCIDRs is the closed list of network ranges a webhook target
// is allowed to resolve into. Anything outside is rejected at create time
// AND again at delivery time as a defence-in-depth against TOCTOU.
//
// 127.0.0.0/8        loopback (incl. the docker host's lo)
// 10.0.0.0/8         RFC 1918 private
// 172.16.0.0/12      RFC 1918 private (default docker bridge subnet too)
// 192.168.0.0/16     RFC 1918 private
// 169.254.0.0/16     link-local (intentionally excluded — it would let a
//                    target hit cloud metadata services; we omit it)
// ::1/128            IPv6 loopback
// fc00::/7           IPv6 unique local
var AllowedWebhookCIDRs = []string{
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"::1/128",
	"fc00::/7",
}

// AllowedWebhookHostnames are friendly names that always resolve. The
// docker-compose network gives services hostnames matching their service
// name (`db`, `app`, `tests`, etc.); operators can add more here.
var AllowedWebhookHostnames = []string{
	"localhost",
	"db",
	"app",
	"tests",
}

// ValidateWebhookTargetURL parses, validates, and verifies that a webhook
// target URL points at a local-network destination. The check uses the
// CIDR allow-list above (anything outside is rejected) plus a small
// hostname allow-list for the docker-compose service names.
//
// Returns the parsed *url.URL on success so callers can persist the
// canonicalized form.
func ValidateWebhookTargetURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("target_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("scheme %q not allowed (http/https only)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("missing host")
	}
	// Hostname allow-list bypasses DNS resolution so the docker compose
	// network names always work without going through resolv.conf.
	for _, h := range AllowedWebhookHostnames {
		if strings.EqualFold(host, h) {
			return u, nil
		}
	}
	// Otherwise resolve and verify each returned IP is in an allowed CIDR.
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses for %q", host)
	}
	for _, ip := range addrs {
		if !isLocalIP(ip) {
			return nil, fmt.Errorf("host %q resolves to non-local address %s; webhook targets must be local-network only", host, ip.String())
		}
	}
	return u, nil
}

func isLocalIP(ip net.IP) bool {
	for _, cidr := range AllowedWebhookCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

type WebhookService struct {
	repo   repository.WebhookRepository
	client *http.Client
	log    *slog.Logger
}

func NewWebhookService(repo repository.WebhookRepository, log *slog.Logger) *WebhookService {
	return &WebhookService{
		repo:   repo,
		client: &http.Client{Timeout: 5 * time.Second},
		log:    log,
	}
}

func (s *WebhookService) Create(ctx context.Context, w *domain.Webhook) (*domain.Webhook, error) {
	if w.Name == "" {
		return nil, errors.Join(domain.ErrInvalidInput, errors.New("name is required"))
	}
	parsed, err := ValidateWebhookTargetURL(w.TargetURL)
	if err != nil {
		return nil, errors.Join(domain.ErrInvalidInput, err)
	}
	w.TargetURL = parsed.String()
	if !w.Enabled {
		w.Enabled = true
	}
	if err := s.repo.Create(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WebhookService) List(ctx context.Context) ([]domain.Webhook, error) {
	return s.repo.List(ctx)
}

func (s *WebhookService) Disable(ctx context.Context, id uuid.UUID) error {
	return s.repo.Disable(ctx, id)
}

// Emit fans out an event to all matching enabled webhooks. Each delivery is
// queued; the actual HTTP call happens in RunDeliveryCycle so the request
// path stays fast and retries can use exponential backoff.
func (s *WebhookService) Emit(ctx context.Context, eventType string, payload any) error {
	hooks, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, h := range hooks {
		if !h.Enabled {
			continue
		}
		if !filterAccepts(h.EventFilter, eventType) {
			continue
		}
		// Apply field mapping: rename top-level keys per the mapping config.
		mapped := applyFieldMapping(body, h.FieldMapping)
		_ = s.repo.EnqueueDelivery(ctx, &domain.WebhookDelivery{
			WebhookID:     h.ID,
			EventType:     eventType,
			Payload:       mapped,
			NextAttemptAt: now,
			Status:        "pending",
		})
	}
	return nil
}

// RunDeliveryCycle is invoked periodically by the jobs runner. It pulls
// pending deliveries that are due, attempts each one, and reschedules with
// exponential backoff up to 5 attempts before marking 'dead'.
func (s *WebhookService) RunDeliveryCycle(ctx context.Context) error {
	now := time.Now().UTC()
	pending, err := s.repo.DequeuePending(ctx, now, 25)
	if err != nil {
		return err
	}
	for _, d := range pending {
		s.attempt(ctx, d)
	}
	return nil
}

func (s *WebhookService) attempt(ctx context.Context, d domain.WebhookDelivery) {
	hook, err := s.repo.Get(ctx, d.WebhookID)
	if err != nil {
		_ = s.repo.UpdateDeliveryAttempt(ctx, d.ID, d.Attempts+1, "failed", "missing webhook", time.Now().UTC().Add(time.Hour))
		return
	}
	// Defence-in-depth: re-validate before each delivery so a row mutated
	// directly in the database (or via a stale create-time check) cannot
	// turn into an SSRF target.
	if _, vErr := ValidateWebhookTargetURL(hook.TargetURL); vErr != nil {
		s.handleFailure(ctx, d.ID, d.Attempts+1, "blocked: "+vErr.Error())
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.TargetURL, bytes.NewReader(d.Payload))
	if err != nil {
		_ = s.repo.UpdateDeliveryAttempt(ctx, d.ID, d.Attempts+1, "failed", err.Error(), time.Now().UTC().Add(time.Hour))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HarborWorks-Event", d.EventType)
	if hook.Secret != "" {
		req.Header.Set("X-HarborWorks-Secret", hook.Secret)
	}

	resp, err := s.client.Do(req)
	attempts := d.Attempts + 1
	if err != nil {
		s.handleFailure(ctx, d.ID, attempts, "transport error: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = s.repo.UpdateDeliveryAttempt(ctx, d.ID, attempts, "delivered", fmt.Sprintf("%d %s", resp.StatusCode, string(body)), time.Now().UTC())
		return
	}
	s.handleFailure(ctx, d.ID, attempts, fmt.Sprintf("%d %s", resp.StatusCode, string(body)))
}

// WebhookBackoff returns the duration to wait before retry attempt N. Pure
// function so unit tests can validate the schedule (1s, 2s, 4s, 8s, 16s).
func WebhookBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	return time.Duration(1<<uint(attempts-1)) * time.Second
}

// NextWebhookStatus returns the status that should be persisted after the
// given attempt fails. After WebhookMaxAttempts the delivery is dead.
func NextWebhookStatus(attempts int) string {
	if attempts >= WebhookMaxAttempts {
		return "dead"
	}
	return "pending"
}

func (s *WebhookService) handleFailure(ctx context.Context, id uuid.UUID, attempts int, reason string) {
	status := NextWebhookStatus(attempts)
	if status == "dead" {
		_ = s.repo.UpdateDeliveryAttempt(ctx, id, attempts, "dead", reason, time.Now().UTC())
		return
	}
	_ = s.repo.UpdateDeliveryAttempt(ctx, id, attempts, "pending", reason, time.Now().UTC().Add(WebhookBackoff(attempts)))
}

func (s *WebhookService) Deliveries(ctx context.Context, limit int) ([]domain.WebhookDelivery, error) {
	return s.repo.ListDeliveries(ctx, limit)
}

// ---------- helpers ----------

func filterAccepts(filter []string, eventType string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if f == "*" || f == eventType {
			return true
		}
	}
	return false
}

// applyFieldMapping renames top-level fields in a JSON payload using the
// supplied map (source -> target). Unknown source fields are left untouched.
func applyFieldMapping(body []byte, mapping map[string]string) []byte {
	if len(mapping) == 0 {
		return body
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	for src, tgt := range mapping {
		if v, ok := m[src]; ok {
			m[tgt] = v
			if src != tgt {
				delete(m, src)
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}
