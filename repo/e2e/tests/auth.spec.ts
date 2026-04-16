/**
 * Auth page & flow tests.
 *
 * Uses Playwright's APIRequestContext (no browser required). Tests cover:
 *   - HTML structure of server-rendered login and register pages
 *   - Password-policy enforcement at registration
 *   - Invalid credential rejection at login
 *   - Full register → login → /me round trip
 *   - Logout invalidates the session
 */

import { test, expect } from '@playwright/test';

// ─── Page rendering ───────────────────────────────────────────────────────────

test('GET /auth/login returns 200 with HTML form', async ({ request }) => {
  const res = await request.get('/auth/login');
  expect(res.status()).toBe(200);
  const html = await res.text();
  expect(html).toContain('<form');
  expect(html).toContain('username');
  expect(html).toContain('password');
  expect(html).toContain('Log in');
});

test('GET /auth/register returns 200 with HTML form', async ({ request }) => {
  const res = await request.get('/auth/register');
  expect(res.status()).toBe(200);
  const html = await res.text();
  expect(html).toContain('<form');
  expect(html).toContain('username');
  expect(html).toContain('password');
  expect(html).toContain('Register');
});

test('login page mentions password policy', async ({ request }) => {
  const res = await request.get('/auth/register');
  expect(res.status()).toBe(200);
  const html = await res.text();
  // The register page describes the policy inline.
  expect(html.toLowerCase()).toMatch(/12|policy|uppercase|lowercase/);
});

// ─── Registration policy enforcement ─────────────────────────────────────────

test('POST /api/auth/register rejects weak password with 400', async ({ request }) => {
  const res = await request.post('/api/auth/register', {
    headers: { 'Content-Type': 'application/json' },
    data: { username: `weakpwd_${Date.now()}`, password: 'short' },
  });
  expect(res.status()).toBe(400);
});

test('POST /api/auth/register rejects missing username with 400', async ({ request }) => {
  const res = await request.post('/api/auth/register', {
    headers: { 'Content-Type': 'application/json' },
    data: { username: '', password: 'StrongPass@2026!' },
  });
  expect(res.status()).toBe(400);
});

test('POST /api/auth/register accepts valid credentials with 201', async ({ request }) => {
  const res = await request.post('/api/auth/register', {
    headers: { 'Content-Type': 'application/json' },
    data: { username: `reguser_${Date.now()}`, password: 'Harbor@Reg2026!' },
  });
  expect(res.status()).toBe(201);
});

// ─── Login policy enforcement ─────────────────────────────────────────────────

test('POST /api/auth/login rejects unknown user with 401', async ({ request }) => {
  const res = await request.post('/api/auth/login', {
    headers: { 'Content-Type': 'application/json' },
    data: { username: 'nobody_xyz_9999', password: 'Harbor@Wrong2026!' },
  });
  expect(res.status()).toBe(401);
});

test('POST /api/auth/login rejects wrong password with 401', async ({ request }) => {
  // Register a user first, then try the wrong password.
  const username = `logintest_${Date.now()}`;
  const password = 'Harbor@Login2026!';
  await request.post('/api/auth/register', {
    headers: { 'Content-Type': 'application/json' },
    data: { username, password },
  });
  const res = await request.post('/api/auth/login', {
    headers: { 'Content-Type': 'application/json' },
    data: { username, password: 'Harbor@Wrong2026!' },
  });
  expect(res.status()).toBe(401);
});

// ─── Full auth round trip ──────────────────────────────────────────────────────

test('register → login → /me → logout round trip', async ({ request }) => {
  const username = `e2e_${Date.now()}`;
  const password = 'E2eHarbor@2026!';

  // Register.
  const reg = await request.post('/api/auth/register', {
    headers: { 'Content-Type': 'application/json' },
    data: { username, password },
  });
  expect(reg.status(), 'registration should succeed').toBe(201);

  // Login.
  const login = await request.post('/api/auth/login', {
    headers: { 'Content-Type': 'application/json' },
    data: { username, password },
  });
  expect(login.status(), 'login should return 200').toBe(200);

  // Login response: { user: {...}, session_expires_at: "..." }
  const loginBody = await login.json();
  expect(loginBody.user.username).toBe(username);

  // /me confirms the session is valid.
  const me = await request.get('/api/auth/me');
  expect(me.status(), '/me should return 200').toBe(200);
  const meBody = await me.json();
  expect(meBody.username).toBe(username);

  // Logout.
  const logout = await request.post('/api/auth/logout');
  expect(logout.status(), 'logout should return 200').toBe(200);

  // /me after logout should be 401.
  const meAfter = await request.get('/api/auth/me');
  expect(meAfter.status(), '/me after logout should be 401').toBe(401);
});

// ─── Seeded demo credentials ──────────────────────────────────────────────────

test('seeded admin user can log in', async ({ request }) => {
  const res = await request.post('/api/auth/login', {
    headers: { 'Content-Type': 'application/json' },
    data: { username: 'admin', password: 'Admin@Harbor2026!' },
  });
  expect(res.status(), 'admin login should succeed').toBe(200);
  // Login response wraps the user under "user": { user: {...}, session_expires_at: "..." }
  const body = await res.json();
  expect(body.user.is_admin).toBe(true);
});

test('seeded demo user can log in', async ({ request }) => {
  const res = await request.post('/api/auth/login', {
    headers: { 'Content-Type': 'application/json' },
    data: { username: 'demouser', password: 'User@Harbor2026!' },
  });
  expect(res.status(), 'demouser login should succeed').toBe(200);
});
