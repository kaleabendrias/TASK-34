package cache

import (
	"sync"
	"time"
)

// DefaultTTL is the global cache TTL mandated by policy: 60 seconds.
const DefaultTTL = 60 * time.Second

// Entry holds a cached payload and the moment it stops being valid.
type Entry struct {
	Value     []byte
	ExpiresAt time.Time
}

// Cache is a tiny in-process TTL cache. It is intentionally simple: a single
// mutex-guarded map keyed by string with monotonic expiry. The middleware
// layer is responsible for hashing requests into stable keys.
type Cache struct {
	mu  sync.RWMutex
	m   map[string]Entry
	ttl time.Duration
}

func New(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	c := &Cache{m: make(map[string]Entry), ttl: ttl}
	go c.gcLoop()
	return c
}

func (c *Cache) TTL() time.Duration { return c.ttl }

// Get returns the value if present and not expired. The bool reports a hit.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(e.ExpiresAt) {
		c.mu.Lock()
		delete(c.m, key)
		c.mu.Unlock()
		return nil, false
	}
	return e.Value, true
}

// Set stores a value with the cache's TTL.
func (c *Cache) Set(key string, value []byte) {
	c.mu.Lock()
	c.m[key] = Entry{Value: value, ExpiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// Invalidate drops a single key (admin bypass + write paths).
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
}

// Purge wipes everything.
func (c *Cache) Purge() {
	c.mu.Lock()
	c.m = make(map[string]Entry)
	c.mu.Unlock()
}

// Stats returns the current entry count and TTL — useful for /admin pages.
func (c *Cache) Stats() (int, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m), c.ttl
}

func (c *Cache) gcLoop() {
	tick := time.NewTicker(c.ttl)
	defer tick.Stop()
	for range tick.C {
		now := time.Now()
		c.mu.Lock()
		for k, e := range c.m {
			if now.After(e.ExpiresAt) {
				delete(c.m, k)
			}
		}
		c.mu.Unlock()
	}
}
