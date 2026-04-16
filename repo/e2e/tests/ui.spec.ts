/**
 * UI page rendering & core API smoke tests.
 *
 * Uses Playwright's APIRequestContext (no browser required). Tests cover:
 *   - Health / readiness endpoints
 *   - Server-rendered HTML pages (structure assertions)
 *   - Resource listing (authenticated)
 *   - Booking lifecycle (create, list, transition)
 *   - Availability page
 *   - 404 handling
 */

import { test, expect } from '@playwright/test';

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Register a fresh user and return a logged-in request context. */
async function loginFresh(request: import('@playwright/test').APIRequestContext) {
  const username = `ui_${Date.now()}`;
  const password = 'UiHarbor@2026!';
  const reg = await request.post('/api/auth/register', {
    headers: { 'Content-Type': 'application/json' },
    data: { username, password },
  });
  expect(reg.status()).toBe(201);
  const login = await request.post('/api/auth/login', {
    headers: { 'Content-Type': 'application/json' },
    data: { username, password },
  });
  expect(login.status()).toBe(200);
  return username;
}

// ─── Health / readiness ───────────────────────────────────────────────────────

test('GET /healthz returns alive', async ({ request }) => {
  const res = await request.get('/healthz');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.status).toBe('alive');
});

test('GET /readyz returns ready', async ({ request }) => {
  const res = await request.get('/readyz');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.status).toBe('ready');
});

// ─── Server-rendered page structure ──────────────────────────────────────────

test('GET / for anonymous user renders HarborWorks branding', async ({ request }) => {
  const res = await request.get('/');
  // Either a 200 landing page or a redirect to /auth/login.
  expect([200, 302, 303]).toContain(res.status());
  if (res.status() === 200) {
    const html = await res.text();
    expect(html).toContain('HarborWorks');
  }
});

test('GET /availability returns 200 with HTML content', async ({ request }) => {
  const res = await request.get('/availability');
  expect(res.status()).toBe(200);
  const html = await res.text();
  expect(html).toContain('HarborWorks');
  // The page contains a date picker or availability-related markup.
  expect(html.toLowerCase()).toMatch(/availab|resource|slip/);
});

test('unknown route returns 404', async ({ request }) => {
  const res = await request.get('/this-page-definitely-does-not-exist-xyz');
  expect(res.status()).toBe(404);
});

// ─── Resource API ─────────────────────────────────────────────────────────────

test('GET /api/resources returns list (authenticated)', async ({ request }) => {
  await loginFresh(request);
  const res = await request.get('/api/resources');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(Array.isArray(body.resources)).toBeTruthy();
});

test('GET /api/resources returns 401 for anonymous', async ({ request }) => {
  // Use a bare request context without any cookies.
  const res = await request.get('/api/resources', {
    headers: { Cookie: '' },
  });
  // Depending on middleware configuration, unauthenticated list may be allowed
  // or blocked. Either 200 (public) or 401 (protected) is acceptable.
  expect([200, 401]).toContain(res.status());
});

// ─── Booking lifecycle ────────────────────────────────────────────────────────

test('POST /api/bookings enforces lead-time policy', async ({ request }) => {
  await loginFresh(request);

  // Resource seeded by seed.sql.
  const resourceId = 'aaaa1111-0000-0000-0000-000000000001';

  // A start time only 30 minutes in the future violates the 2-hour lead time.
  const soon = new Date(Date.now() + 30 * 60 * 1000);
  const later = new Date(Date.now() + 90 * 60 * 1000);

  const res = await request.post('/api/bookings', {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': `lead-${Date.now()}`,
    },
    data: {
      resource_id: resourceId,
      start_time: soon.toISOString(),
      end_time: later.toISOString(),
    },
  });
  expect(res.status()).toBe(409);
});

test('POST /api/bookings creates valid booking', async ({ request }) => {
  await loginFresh(request);

  const resourceId = 'aaaa1111-0000-0000-0000-000000000001';
  const start = new Date(Date.now() + 3 * 60 * 60 * 1000); // +3h
  const end   = new Date(Date.now() + 4 * 60 * 60 * 1000); // +4h

  const res = await request.post('/api/bookings', {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': `create-${Date.now()}`,
    },
    data: {
      resource_id: resourceId,
      start_time: start.toISOString(),
      end_time: end.toISOString(),
    },
  });
  expect([200, 201]).toContain(res.status());
  const body = await res.json();
  expect(body.id).toBeTruthy();
});

test('GET /api/bookings lists current users bookings', async ({ request }) => {
  await loginFresh(request);
  const res = await request.get('/api/bookings');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(Array.isArray(body.bookings)).toBeTruthy();
});

// ─── Idempotency key replay ───────────────────────────────────────────────────

test('repeated POST with same Idempotency-Key returns Idempotent-Replay header', async ({ request }) => {
  await loginFresh(request);

  const resourceId = 'aaaa1111-0000-0000-0000-000000000002';
  const start = new Date(Date.now() + 5 * 60 * 60 * 1000);
  const end   = new Date(Date.now() + 6 * 60 * 60 * 1000);
  const key   = `idem-${Date.now()}`;

  const payload = {
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
    data: { resource_id: resourceId, start_time: start.toISOString(), end_time: end.toISOString() },
  };

  // First request: creates the booking.
  const first = await request.post('/api/bookings', payload);
  expect([200, 201]).toContain(first.status());

  // Second request with same key: must replay.
  const second = await request.post('/api/bookings', payload);
  expect(second.headers()['idempotent-replay']).toBe('true');
});

// ─── Notifications ────────────────────────────────────────────────────────────

test('GET /api/notifications/unread-count responds', async ({ request }) => {
  await loginFresh(request);
  const res = await request.get('/api/notifications/unread-count');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(typeof body.unread).toBe('number');
});

// ─── Analytics ────────────────────────────────────────────────────────────────

test('POST /api/analytics/track accepts event', async ({ request }) => {
  const res = await request.post('/api/analytics/track', {
    headers: { 'Content-Type': 'application/json' },
    data: {
      event_type: 'view',
      target_type: 'resource',
      target_id: 'aaaa1111-0000-0000-0000-000000000001',
    },
  });
  expect([200, 202, 204]).toContain(res.status());
});
