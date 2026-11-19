package auth

import (
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	m := NewSessionManager(time.Hour)
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	token, _, err := m.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !m.Touch(token) {
		t.Fatal("fresh session should be valid")
	}
	now = now.Add(2 * time.Hour)
	if m.Touch(token) {
		t.Fatal("expired session should be invalid")
	}

	token, _, _ = m.Create()
	m.Revoke(token)
	if m.Touch(token) {
		t.Fatal("revoked session should be invalid")
	}
}

func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter(2, time.Minute)
	now := time.Unix(2000, 0)
	l.now = func() time.Time { return now }
	if !l.Allow("a") {
		t.Fatal("first attempt should pass")
	}
	if !l.Allow("a") {
		t.Fatal("second attempt should pass")
	}
	if l.Allow("a") {
		t.Fatal("third attempt should be blocked")
	}
	if !l.Allow("b") {
		t.Fatal("different key should have its own budget")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allow("a") {
		t.Fatal("attempts should expire")
	}
}

func TestAuthCache(t *testing.T) {
	c := NewAuthCache(time.Minute)
	now := time.Unix(3000, 0)
	c.now = func() time.Time { return now }
	digest := Digest("secret")
	if c.Allowed(digest) {
		t.Fatal("empty cache should miss")
	}
	c.Remember(digest)
	if !c.Allowed(digest) {
		t.Fatal("remembered password should hit")
	}
	now = now.Add(2 * time.Minute)
	if c.Allowed(digest) {
		t.Fatal("cache entry should expire")
	}
}
