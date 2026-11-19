package auth

import (
	"sync"
	"time"
)

type RateLimiter struct {
	mu     sync.Mutex
	events map[string][]time.Time
	limit  int
	window time.Duration
	now    func() time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		events: make(map[string][]time.Time),
		limit:  limit,
		window: window,
		now:    time.Now,
	}
}

// Allow records an attempt for key and reports whether it is within the limit.
func (l *RateLimiter) Allow(key string) bool {
	now := l.now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()
	events := l.events[key]
	kept := events[:0]
	for _, t := range events {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.events[key] = kept
		return false
	}
	l.events[key] = append(kept, now)
	if len(l.events) > 4096 {
		l.cleanupLocked(cutoff)
	}
	return true
}

func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.events, key)
}

func (l *RateLimiter) cleanupLocked(cutoff time.Time) {
	for key, events := range l.events {
		if len(events) == 0 || events[len(events)-1].Before(cutoff) {
			delete(l.events, key)
		}
	}
}

// AuthCache remembers recently verified Basic-auth passwords so WebDAV does
// not pay the PBKDF2 cost on every request.
type AuthCache struct {
	mu      sync.Mutex
	entries map[[32]byte]time.Time
	ttl     time.Duration
	now     func() time.Time
}

func NewAuthCache(ttl time.Duration) *AuthCache {
	return &AuthCache{
		entries: make(map[[32]byte]time.Time),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (c *AuthCache) Allowed(digest [32]byte) bool {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	expires, ok := c.entries[digest]
	if !ok || !now.Before(expires) {
		delete(c.entries, digest)
		return false
	}
	return true
}

func (c *AuthCache) Remember(digest [32]byte) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > 64 {
		for key, expires := range c.entries {
			if !now.Before(expires) {
				delete(c.entries, key)
			}
		}
	}
	c.entries[digest] = now.Add(c.ttl)
}
