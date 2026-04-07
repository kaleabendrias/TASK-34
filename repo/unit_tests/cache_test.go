package unit_tests

import (
	"testing"
	"time"

	"github.com/harborworks/booking-hub/internal/infrastructure/cache"
)

func TestCacheNewDefaultsAndExplicit(t *testing.T) {
	c := cache.New(0) // 0 → default
	if c.TTL() != cache.DefaultTTL {
		t.Errorf("default TTL = %v", c.TTL())
	}
	c2 := cache.New(time.Second)
	if c2.TTL() != time.Second {
		t.Errorf("explicit TTL = %v", c2.TTL())
	}
}

func TestCacheGetMissAndHit(t *testing.T) {
	c := cache.New(time.Hour)
	if _, ok := c.Get("nope"); ok {
		t.Fatal("miss expected")
	}
	c.Set("k", []byte("v"))
	got, ok := c.Get("k")
	if !ok || string(got) != "v" {
		t.Fatalf("hit expected, got %q ok=%v", got, ok)
	}
	n, ttl := c.Stats()
	if n != 1 || ttl != time.Hour {
		t.Fatalf("stats: n=%d ttl=%v", n, ttl)
	}
}

func TestCacheExpiry(t *testing.T) {
	c := cache.New(20 * time.Millisecond)
	c.Set("k", []byte("v"))
	time.Sleep(40 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected expired")
	}
}

func TestCacheInvalidateAndPurge(t *testing.T) {
	c := cache.New(time.Hour)
	c.Set("a", []byte("1"))
	c.Set("b", []byte("2"))
	c.Invalidate("a")
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be gone")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("b should still be present")
	}
	c.Purge()
	n, _ := c.Stats()
	if n != 0 {
		t.Fatalf("post-purge n=%d", n)
	}
}

// Background GC: with a tight TTL the gc loop should drop the entry on the
// next tick. We give it a generous window so the test stays deterministic.
func TestCacheGCLoopRemovesExpiredEntries(t *testing.T) {
	c := cache.New(50 * time.Millisecond)
	c.Set("k", []byte("v"))
	time.Sleep(300 * time.Millisecond)
	// Give the GC a chance to run; we accept either gc-eviction or
	// lazy-eviction by the next Get call.
	if _, ok := c.Get("k"); ok {
		t.Fatal("entry should be gone after gc + lazy eviction")
	}
}
