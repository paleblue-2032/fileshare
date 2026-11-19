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

func TestSessionAbsoluteLifetime(t *testing.T) {
	m := NewSessionManager(24 * time.Hour)
	m.maxAge = 90 * 24 * time.Hour
	now := time.Unix(2000, 0)
	m.now = func() time.Time { return now }

	token, _, err := m.Create()
	if err != nil {
		t.Fatal(err)
	}
	if !m.Touch(token) {
		t.Fatal("fresh session should be valid")
	}
	now = now.Add(91 * 24 * time.Hour)
	if m.Touch(token) {
		t.Fatal("session must not outlive the absolute cap")
	}
}

func TestRevokeAllSessions(t *testing.T) {
	m := NewSessionManager(time.Hour)
	first, _, _ := m.Create()
	second, _, _ := m.Create()
	m.RevokeAll()
	if m.Count() != 0 {
		t.Fatalf("session count after RevokeAll = %d", m.Count())
	}
	if m.Touch(first) || m.Touch(second) {
		t.Fatal("RevokeAll should invalidate every session")
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
