package unit_tests

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
)

// ─── User Repository Mock ────────────────────────────────────────────────────

type mockUserRepo struct {
	mu         sync.Mutex
	users      map[uuid.UUID]*domain.User
	byUsername map[string]*domain.User

	// Injected errors (set to non-nil to force that method to fail).
	createErr            error
	getByIDErr           error
	getByUsernameErr     error
	recordFailedLoginErr error
	resetFailedLoginErr  error
	updatePasswordErr    error
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:      make(map[uuid.UUID]*domain.User),
		byUsername: make(map[string]*domain.User),
	}
}

func (r *mockUserRepo) seed(u *domain.User) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	r.users[u.ID] = u
	r.byUsername[u.Username] = u
}

func (r *mockUserRepo) Create(ctx context.Context, u *domain.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	u.CreatedAt = time.Now().UTC()
	u.UpdatedAt = u.CreatedAt
	r.users[u.ID] = u
	r.byUsername[u.Username] = u
	return nil
}

func (r *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *mockUserRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	if r.getByUsernameErr != nil {
		return nil, r.getByUsernameErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byUsername[username]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *mockUserRepo) RecordFailedLogin(ctx context.Context, id uuid.UUID, lockUntil *time.Time) error {
	if r.recordFailedLoginErr != nil {
		return r.recordFailedLoginErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.FailedAttempts++
	if lockUntil != nil {
		u.LockedUntil = lockUntil
	}
	return nil
}

func (r *mockUserRepo) ResetFailedLogin(ctx context.Context, id uuid.UUID, loginAt time.Time) error {
	if r.resetFailedLoginErr != nil {
		return r.resetFailedLoginErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.FailedAttempts = 0
	u.LockedUntil = nil
	u.LastLoginAt = &loginAt
	return nil
}

func (r *mockUserRepo) SetBlacklist(ctx context.Context, id uuid.UUID, blacklisted bool, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.IsBlacklisted = blacklisted
	u.BlacklistReason = reason
	return nil
}

func (r *mockUserRepo) SetAdmin(ctx context.Context, id uuid.UUID, admin bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.IsAdmin = admin
	return nil
}

func (r *mockUserRepo) SetMustRotatePassword(ctx context.Context, id uuid.UUID, must bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.MustRotatePassword = must
	return nil
}

func (r *mockUserRepo) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	if r.updatePasswordErr != nil {
		return r.updatePasswordErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.PasswordHash = hash
	u.MustRotatePassword = false
	return nil
}

func (r *mockUserRepo) HardDelete(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	delete(r.byUsername, u.Username)
	delete(r.users, id)
	return nil
}

// ─── Session Repository Mock ─────────────────────────────────────────────────

type mockSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*domain.Session

	createErr error
	getErr    error
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{sessions: make(map[string]*domain.Session)}
}

func (r *mockSessionRepo) seed(s *domain.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
}

func (r *mockSessionRepo) Create(ctx context.Context, s *domain.Session) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
	return nil
}

func (r *mockSessionRepo) Get(ctx context.Context, id string) (*domain.Session, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *mockSessionRepo) Touch(ctx context.Context, id string, at time.Time, newExpires time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil
	}
	s.LastActivityAt = at
	s.ExpiresAt = newExpires
	return nil
}

func (r *mockSessionRepo) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
	return nil
}

func (r *mockSessionRepo) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for id, s := range r.sessions {
		if s.ExpiresAt.Before(before) {
			delete(r.sessions, id)
			n++
		}
	}
	return n, nil
}

// ─── Captcha Repository Mock ─────────────────────────────────────────────────

type mockCaptchaRepo struct {
	mu       sync.Mutex
	captchas map[string]*domain.CaptchaChallenge

	createErr error
	getErr    error
}

func newMockCaptchaRepo() *mockCaptchaRepo {
	return &mockCaptchaRepo{captchas: make(map[string]*domain.CaptchaChallenge)}
}

func (r *mockCaptchaRepo) Create(ctx context.Context, c *domain.CaptchaChallenge) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captchas[c.Token] = c
	return nil
}

func (r *mockCaptchaRepo) Get(ctx context.Context, token string) (*domain.CaptchaChallenge, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.captchas[token]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *mockCaptchaRepo) Consume(ctx context.Context, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.captchas[token]
	if !ok {
		return domain.ErrNotFound
	}
	c.Consumed = true
	return nil
}

// ─── Booking Repository Mock ─────────────────────────────────────────────────

type mockBookingRepo struct {
	mu       sync.Mutex
	bookings map[uuid.UUID]*domain.Booking

	// Control return values for specific queries
	countActiveReturn   int
	countActiveErr      error
	userOverlapReturn   bool
	userOverlapErr      error
	resourceOverlapReturn bool
	resourceOverlapErr  error
	createErr           error
	getErr              error
	updateStatusErr     error
	sumActiveParty      int
}

func newMockBookingRepo() *mockBookingRepo {
	return &mockBookingRepo{bookings: make(map[uuid.UUID]*domain.Booking)}
}

func (r *mockBookingRepo) Create(ctx context.Context, b *domain.Booking) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	b.CreatedAt = time.Now().UTC()
	b.UpdatedAt = b.CreatedAt
	cp := *b
	r.bookings[b.ID] = &cp
	return nil
}

func (r *mockBookingRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bookings[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *b
	return &cp, nil
}

func (r *mockBookingRepo) List(ctx context.Context, limit, offset int) ([]domain.Booking, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Booking, 0, len(r.bookings))
	for _, b := range r.bookings {
		out = append(out, *b)
	}
	return out, nil
}

func (r *mockBookingRepo) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Booking, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Booking, 0)
	for _, b := range r.bookings {
		if b.UserID == userID {
			out = append(out, *b)
		}
	}
	return out, nil
}

func (r *mockBookingRepo) ListByGroup(ctx context.Context, groupID uuid.UUID) ([]domain.Booking, error) {
	return nil, nil
}

func (r *mockBookingRepo) ListByResourceOnDate(ctx context.Context, resourceID uuid.UUID, day time.Time) ([]domain.Booking, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Booking, 0)
	for _, b := range r.bookings {
		if b.ResourceID == resourceID {
			out = append(out, *b)
		}
	}
	return out, nil
}

func (r *mockBookingRepo) CountActiveByUserOnDate(ctx context.Context, userID uuid.UUID, day time.Time) (int, error) {
	if r.countActiveErr != nil {
		return 0, r.countActiveErr
	}
	return r.countActiveReturn, nil
}

func (r *mockBookingRepo) UserHasOverlap(ctx context.Context, userID uuid.UUID, start, end time.Time, exclude *uuid.UUID) (bool, error) {
	if r.userOverlapErr != nil {
		return false, r.userOverlapErr
	}
	return r.userOverlapReturn, nil
}

func (r *mockBookingRepo) ResourceHasOverlap(ctx context.Context, resourceID uuid.UUID, start, end time.Time, exclude *uuid.UUID) (bool, error) {
	if r.resourceOverlapErr != nil {
		return false, r.resourceOverlapErr
	}
	return r.resourceOverlapReturn, nil
}

func (r *mockBookingRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.BookingStatus) error {
	if r.updateStatusErr != nil {
		return r.updateStatusErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bookings[id]
	if !ok {
		return domain.ErrNotFound
	}
	b.Status = status
	return nil
}

func (r *mockBookingRepo) Delete(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bookings, id)
	return nil
}

// ─── Resource Repository Mock ─────────────────────────────────────────────────

type mockResourceRepo struct {
	mu        sync.Mutex
	resources map[uuid.UUID]*domain.Resource

	getErr         error
	sumActiveParty int
	sumActiveErr   error
}

func newMockResourceRepo() *mockResourceRepo {
	return &mockResourceRepo{resources: make(map[uuid.UUID]*domain.Resource)}
}

func (r *mockResourceRepo) seed(res *domain.Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	r.resources[res.ID] = res
}

func (r *mockResourceRepo) List(ctx context.Context) ([]domain.Resource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Resource, 0, len(r.resources))
	for _, res := range r.resources {
		out = append(out, *res)
	}
	return out, nil
}

func (r *mockResourceRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Resource, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	res, ok := r.resources[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *res
	return &cp, nil
}

func (r *mockResourceRepo) Create(ctx context.Context, res *domain.Resource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	r.resources[res.ID] = res
	return nil
}

func (r *mockResourceRepo) InsertManyTx(ctx context.Context, rows []domain.Resource) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range rows {
		if rows[i].ID == uuid.Nil {
			rows[i].ID = uuid.New()
		}
		cp := rows[i]
		r.resources[cp.ID] = &cp
	}
	return len(rows), nil
}

func (r *mockResourceRepo) SumActivePartySizesInWindow(ctx context.Context, resourceID uuid.UUID, start, end time.Time) (int, error) {
	if r.sumActiveErr != nil {
		return 0, r.sumActiveErr
	}
	return r.sumActiveParty, nil
}

// ─── Notification Repository Mock ────────────────────────────────────────────

type mockNotifRepo struct {
	mu             sync.Mutex
	notifications  map[uuid.UUID]*domain.Notification
	todos          map[uuid.UUID]*domain.Todo
	deliveries     []*domain.NotificationDelivery
	createNotifErr error
}

func newMockNotifRepo() *mockNotifRepo {
	return &mockNotifRepo{
		notifications: make(map[uuid.UUID]*domain.Notification),
		todos:         make(map[uuid.UUID]*domain.Todo),
	}
}

func (r *mockNotifRepo) CreateNotification(ctx context.Context, n *domain.Notification) error {
	if r.createNotifErr != nil {
		return r.createNotifErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	cp := *n
	r.notifications[n.ID] = &cp
	return nil
}

func (r *mockNotifRepo) ListNotifications(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int) ([]domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Notification, 0)
	for _, n := range r.notifications {
		if n.UserID != userID {
			continue
		}
		if unreadOnly && n.ReadAt != nil {
			continue
		}
		out = append(out, *n)
	}
	return out, nil
}

func (r *mockNotifRepo) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.notifications[id]
	if !ok || n.UserID != userID {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	n.ReadAt = &now
	return nil
}

func (r *mockNotifRepo) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var c int
	for _, n := range r.notifications {
		if n.UserID == userID && n.ReadAt == nil {
			c++
		}
	}
	return c, nil
}

func (r *mockNotifRepo) CreateTodo(ctx context.Context, t *domain.Todo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	cp := *t
	r.todos[t.ID] = &cp
	return nil
}

func (r *mockNotifRepo) ListTodos(ctx context.Context, userID uuid.UUID, status string, limit int) ([]domain.Todo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Todo, 0)
	for _, t := range r.todos {
		if t.UserID != userID {
			continue
		}
		if status != "" && string(t.Status) != status {
			continue
		}
		out = append(out, *t)
	}
	return out, nil
}

func (r *mockNotifRepo) UpdateTodoStatus(ctx context.Context, userID, id uuid.UUID, status domain.TodoStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.todos[id]
	if !ok || t.UserID != userID {
		return domain.ErrNotFound
	}
	t.Status = status
	return nil
}

func (r *mockNotifRepo) LogDelivery(ctx context.Context, d *domain.NotificationDelivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	cp := *d
	r.deliveries = append(r.deliveries, &cp)
	return nil
}

func (r *mockNotifRepo) ListDeliveries(ctx context.Context, limit int) ([]domain.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.NotificationDelivery, 0, len(r.deliveries))
	for _, d := range r.deliveries {
		out = append(out, *d)
	}
	return out, nil
}

// ─── Analytics Repository Mock ───────────────────────────────────────────────

type mockAnalyticsRepo struct {
	mu       sync.Mutex
	events   []*domain.AnalyticsEvent
	anomalies []*domain.AnomalyAlert

	hourlyCount        int64
	hourlyCountErr     error
	baselineAvg        float64
	baselineAvgErr     error
	insertAnomalyErr   error
	recordEventErr     error
}

func newMockAnalyticsRepo() *mockAnalyticsRepo {
	return &mockAnalyticsRepo{}
}

func (r *mockAnalyticsRepo) RecordEvent(ctx context.Context, e *domain.AnalyticsEvent) error {
	if r.recordEventErr != nil {
		return r.recordEventErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *e
	r.events = append(r.events, &cp)
	return nil
}

func (r *mockAnalyticsRepo) UpsertHourly(ctx context.Context, h domain.AnalyticsHourly) error {
	return nil
}

func (r *mockAnalyticsRepo) AggregateRecent(ctx context.Context, since time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.events)), nil
}

func (r *mockAnalyticsRepo) TopSessions(ctx context.Context, since time.Time, limit int) ([]domain.TopSession, error) {
	return []domain.TopSession{}, nil
}

func (r *mockAnalyticsRepo) Trend(ctx context.Context, eventType domain.AnalyticsEventType, days int) ([]domain.TrendBucket, error) {
	return []domain.TrendBucket{}, nil
}

func (r *mockAnalyticsRepo) HourlyCount(ctx context.Context, eventType domain.AnalyticsEventType, hourStart time.Time) (int64, error) {
	if r.hourlyCountErr != nil {
		return 0, r.hourlyCountErr
	}
	return r.hourlyCount, nil
}

func (r *mockAnalyticsRepo) BaselineHourlyAverage(ctx context.Context, eventType domain.AnalyticsEventType, hourStart time.Time) (float64, error) {
	if r.baselineAvgErr != nil {
		return 0, r.baselineAvgErr
	}
	return r.baselineAvg, nil
}

func (r *mockAnalyticsRepo) InsertAnomaly(ctx context.Context, a *domain.AnomalyAlert) error {
	if r.insertAnomalyErr != nil {
		return r.insertAnomalyErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	cp := *a
	r.anomalies = append(r.anomalies, &cp)
	return nil
}

func (r *mockAnalyticsRepo) ListAnomalies(ctx context.Context, limit int) ([]domain.AnomalyAlert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.AnomalyAlert, 0, len(r.anomalies))
	for _, a := range r.anomalies {
		out = append(out, *a)
	}
	return out, nil
}

func (r *mockAnalyticsRepo) AnonymiseUserEvents(ctx context.Context, anon string) error {
	return nil
}

// ─── Idempotency Repository Mock ─────────────────────────────────────────────

type mockIdemRepo struct {
	mu      sync.Mutex
	records map[string]*domain.IdempotencyRecord

	// Control behavior for Reserve. When non-nil, Reserve returns this error.
	reserveErr error
	// When set, Reserve returns this completed record (replay scenario).
	replayRecord *domain.IdempotencyRecord
	// When set, Reserve returns this pending record with ErrConflict.
	pendingRecord *domain.IdempotencyRecord
	// When set, Reserve returns mismatch error.
	mismatch bool
}

func newMockIdemRepo() *mockIdemRepo {
	return &mockIdemRepo{records: make(map[string]*domain.IdempotencyRecord)}
}

func (r *mockIdemRepo) Reserve(ctx context.Context, userID *uuid.UUID, key, requestHash string, ttl time.Duration) (*domain.IdempotencyRecord, bool, error) {
	if r.reserveErr != nil {
		return nil, false, r.reserveErr
	}
	if r.mismatch {
		rec := &domain.IdempotencyRecord{Key: key, RequestHash: "different"}
		return rec, false, domain.ErrIdempotencyMismatch
	}
	if r.pendingRecord != nil {
		return r.pendingRecord, false, domain.ErrConflict
	}
	if r.replayRecord != nil {
		return r.replayRecord, false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[key]; exists {
		// Already reserved; simulate completed.
		return r.records[key], false, nil
	}
	rec := &domain.IdempotencyRecord{
		Key:         key,
		RequestHash: requestHash,
		Status:      domain.IdempotencyStatusPending,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(ttl),
	}
	if userID != nil {
		id := *userID
		rec.UserID = &id
	}
	r.records[key] = rec
	return nil, true, nil
}

func (r *mockIdemRepo) Complete(ctx context.Context, userID *uuid.UUID, key string, statusCode int, body []byte, contentType string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[key]
	if !ok {
		return nil
	}
	rec.Status = domain.IdempotencyStatusCompleted
	rec.StatusCode = statusCode
	rec.ResponseBody = body
	rec.ContentType = contentType
	return nil
}

func (r *mockIdemRepo) ReleasePending(ctx context.Context, userID *uuid.UUID, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, key)
	return nil
}

func (r *mockIdemRepo) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	return 0, nil
}

// ─── Notes Encrypter Stub ─────────────────────────────────────────────────────

// stubEncrypter is a trivial XOR-with-key encrypter used in booking service
// unit tests. It satisfies the notesEncrypter interface without requiring the
// full AES-256 GCM stack.
type stubEncrypter struct {
	encryptErr error
	decryptErr error
}

func (e *stubEncrypter) Encrypt(plaintext []byte) ([]byte, error) {
	if e.encryptErr != nil {
		return nil, e.encryptErr
	}
	// Simple reversible transform: XOR each byte with 0xAA.
	out := make([]byte, len(plaintext))
	for i, b := range plaintext {
		out[i] = b ^ 0xAA
	}
	return out, nil
}

func (e *stubEncrypter) Decrypt(ciphertext []byte) ([]byte, error) {
	if e.decryptErr != nil {
		return nil, e.decryptErr
	}
	out := make([]byte, len(ciphertext))
	for i, b := range ciphertext {
		out[i] = b ^ 0xAA
	}
	return out, nil
}
