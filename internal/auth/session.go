package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const CookieName = "fileshare_session"

const maxSessionAge = 90 * 24 * time.Hour

type session struct {
	expires time.Time
	created time.Time
}

type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]session
	ttl      time.Duration
	maxAge   time.Duration
	now      func() time.Time
}

func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]session),
		ttl:      ttl,
		maxAge:   maxSessionAge,
		now:      time.Now,
	}
}

func (m *SessionManager) Create() (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := m.now()
	expires := now.Add(m.ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	m.sessions[token] = session{expires: expires, created: now}
	return token, expires, nil
}

// Touch validates a token, enforces an absolute 90-day cap, and extends the
// sliding lifetime while the device is active.
func (m *SessionManager) Touch(token string) bool {
	if token == "" {
		return false
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[token]
	if !ok {
		return false
	}
	if !now.Before(entry.expires) || now.Sub(entry.created) >= m.maxAge {
		delete(m.sessions, token)
		return false
	}
	if entry.expires.Sub(now) < m.ttl/2 {
		next := now.Add(m.ttl)
		if absolute := entry.created.Add(m.maxAge); next.After(absolute) {
			next = absolute
		}
		entry.expires = next
		m.sessions[token] = entry
	}
	return true
}

func (m *SessionManager) Revoke(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func (m *SessionManager) RevokeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.sessions)
}

func (m *SessionManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *SessionManager) cleanupLocked() {
	if len(m.sessions) < 1000 {
		return
	}
	now := m.now()
	for token, entry := range m.sessions {
		if !now.Before(entry.expires) || now.Sub(entry.created) >= m.maxAge {
			delete(m.sessions, token)
		}
	}
}
